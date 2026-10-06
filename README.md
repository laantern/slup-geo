# slup-geo

**Гео-апплаенс для продуктов на OpenStreetMap.** Один контейнер: PostgreSQL + PostGIS, импорт OSM,
поиск адресов и готовая карта для фронтенда — без внешних гео-API, ключей и лимитов.

[![build](https://github.com/laantern/slup-geo/actions/workflows/build.yml/badge.svg)](https://github.com/laantern/slup-geo/actions/workflows/build.yml)

Снаружи — образ, один volume и ENV. Внутри — два равноценных блока поверх общего движка данных:
**API для бэкенда** и **карта для фронтенда**.

## Что решает

| Задача продукта | Блок | Ответ сервиса |
|---|---|---|
| Подсказать адрес при вводе | **API** | `GET /v1/suggest?q=Бородина` — дома, улицы, районы |
| Понять, где находится точка | **API** | `GET /v1/point?lat&lon` — дом и зоны, готовая подпись |
| Показать границы зон и домов | **API** | `GET /v1/areas/{id}` — GeoJSON |
| Нарисовать карту пользователю | **Карта** | `/tiles/style.json` + PMTiles + шрифты — всё своё |
| Держать данные свежими | **Движок** | PBF → БД и тайлы: вручную, при старте или по cron |

## Быстрый старт

```yaml
services:
  geo:
    image: aliakseikarpenka/slup-geo:latest
    restart: unless-stopped
    # ports:
    #   - "8080:8080"   # только если обращаетесь с хоста; в docker-сети — http://geo:8080
    environment:
      # ОБЯЗАТЕЛЬНОЕ, если нет локального PBF
      PBF_URL: https://download.geofabrik.de/europe/belarus-latest.osm.pbf

      # НЕОБЯЗАТЕЛЬНЫЕ (значения — по умолчанию)
      # PBF_PATH: /data/osm/belarus-latest.osm.pbf   # локальный PBF: кэш/офлайн-импорт
      # UPDATE_ON_START: "false"                     # обновлять данные при каждом старте
      # UPDATE_SCHEDULE: "0 4 * * 1"                 # cron обновлений (пусто = выключено)
      # TILES_ENABLED: "true"                        # собирать карту из PBF (tilemaker)
      # TILEMAKER_THREADS: "0"                       # потоков tilemaker (0 = авто)
      # ENABLE_EXAMPLE: "true"                       # страница-пример /example (в проде можно выключить)
      # IMPORT_PROCESSES: "4"                        # процессов osm2pgsql
      # DATA_DIR: /data                              # БД, PBF, тайлы, состояние
      # HTTP_ADDR: ":8080"
      # LOG_LEVEL: info                              # debug | info | warn | error
    volumes:
      - geo_data:/data

volumes:
  geo_data:
```

Первый запуск скачивает PBF, импортирует его и собирает тайлы — несколько минут. Пока идёт импорт,
сервис ещё не слушает порт; готовность — `{"status":"ok"}`:

```bash
curl http://localhost:8080/health
```

После старта откройте **http://localhost:8080/example** — готовая страница с картой и поиском.

## Блок 1 — API: поиск и гео-данные

Для вашего бэкенда: подсказки, локация точки, границы. Вызывается по внутренней сети
(`http://geo:8080`); публичный контракт, auth, CORS и лимиты остаются на вашей стороне.

| Метод и путь | Что отдаёт |
|---|---|
| `GET /v1/suggest?q` | До 10 подсказок: админ-зоны/улицы → дома улицы → зоны улицы → зоны по имени |
| `GET /v1/point?lat&lon` | Дом (≤5 м от контура) + зоны точки, подпись `text` |
| `GET /v1/areas/{id}?simplify=display\|none` | Границы зоны или контур дома (GeoJSON) |

```bash
curl 'http://localhost:8080/v1/suggest?q=Бородина'
curl 'http://localhost:8080/v1/point?lat=52.3955063&lon=30.9607992'
```

```json
{
  "text": "улица Григория Денисенко, д. 22, Гомель",
  "zones": [
    { "id": "W664945556", "name": "улица Григория Денисенко, д. 22", "level": "HOUSE", "kind": "BUILDING", "areaM2": 1460 },
    { "id": "W-1", "name": "Железнодорожный район", "level": "CITY_DISTRICT", "kind": "ADMIN", "areaM2": 36777954 }
  ]
}
```

Ошибки — Problem Details, машинный код в `type`: `VALIDATION_ERROR`, `NOT_FOUND`,
`INTERNAL_SERVER_ERROR`. Полная спецификация — [openapi.yaml](openapi.yaml).

## Блок 2 — Карта: всё для фронтенда

Равноценный API блок: чтобы нарисовать карту, внешние сервисы не нужны — сервис отдаёт данные,
стиль и шрифты, а рисует браузер (MapLibre GL JS + плагин pmtiles).

| Что | URL | Формат |
|---|---|---|
| Стиль (OSM Bright под схему OpenMapTiles) | `/tiles/style.json` | MapLibre Style JSON |
| Данные карты (версионные, `immutable`) | `/tiles/basemap-*.pmtiles` | PMTiles (внутри MVT) |
| Стабильный адрес актуального архива | `/tiles/basemap.pmtiles` | PMTiles |
| Подписи (глифы Noto) | `/tiles/fonts/...` | pbf |
| Готовая страница-пример | `/example` | HTML |

Подключение — две строки:

```js
maplibregl.addProtocol('pmtiles', new pmtiles.Protocol().tile);
const map = new maplibregl.Map({ container: 'map', style: '/tiles/style.json' });
```

- Всё относительное: работает за любым доменом, nginx и CDN **без настроек**.
- Подписи — на русском (`name:ru`), при отсутствии — исходное имя из OSM.
- Атрибуция обязательна: `© OpenMapTiles · © OpenStreetMap contributors`.
- Библиотеки можно взять вендорно из `/example` (MapLibre, pmtiles) — без CDN.
- Выключается: `TILES_ENABLED=false` (API продолжает работать), страница — `ENABLE_EXAMPLE=false`.

## Блок 3 — Движок данных

```
[OSM PBF] → osm2pgsql → PostgreSQL+PostGIS → geo.* → поисковый API
          ↘ tilemaker → basemap-*.pmtiles → /tiles/* → карта на фронте
```

**`slup-geo update`** (вручную, при старте `UPDATE_ON_START=true` или по cron `UPDATE_SCHEDULE`):

1. скачивание PBF (условный `If-Modified-Since`) или локальный файл;
2. `osm2pgsql --create --hstore-all --latlong` — таблицы `planet_osm_*`;
3. представления `geo.zones/streets/addresses/names` (склейка мультиполигонов, поисковый индекс
   имён) + индексы GIST/`pg_trgm`, `ANALYZE`;
4. `tilemaker` (профиль OpenMapTiles) — векторные тайлы из того же PBF, зоны 0–14;
5. `basemap-<время>.pmtiles` + `tiles.json` + `state.json` — атомарно (temp + rename).

**Ориентиры** для Беларуси (PBF 334 МБ): тайлы ~370 МБ, сборка ~минуты (замер: 40 с на 16 ядрах),
пиковый промежуточный диск ~1 ГБ. **Данные** — один volume `geo_data`: БД, PBF-кэш, тайлы,
состояние; пересоздание контейнера данные не теряет.

### Зависимости и лицензии

| Компонент | Роль | Ссылка | Лицензия |
|---|---|---|---|
| OpenStreetMap (PBF) | данные | [openstreetmap.org](https://www.openstreetmap.org/) | ODbL 1.0 (атрибуция обязательна) |
| PostgreSQL + PostGIS | БД и пространственные функции | [postgresql.org](https://www.postgresql.org/), [postgis.net](https://postgis.net/) | PostgreSQL License / GPL-2+ |
| osm2pgsql | импорт PBF в БД | [osm2pgsql.org](https://osm2pgsql.org/) | GPL-2+ |
| tilemaker | сборка векторных тайлов | [github.com/systemed/tilemaker](https://github.com/systemed/tilemaker) | FTWPL (в Debian — GPL-2+) |
| OpenMapTiles (схема и профиль) | имена слоёв, конфиг tilemaker | [openmaptiles.org/schema](https://openmaptiles.org/schema/) | BSD-3-Clause |
| PMTiles | формат архива тайлов | [github.com/protomaps/PMTiles](https://github.com/protomaps/PMTiles) | спецификация — CC0/public domain, реализации — BSD-3 |
| OSM Bright | стиль карты | [github.com/openmaptiles/osm-bright-gl-style](https://github.com/openmaptiles/osm-bright-gl-style) | код BSD-3, дизайн CC-BY 4.0 |
| Noto (глифы) | шрифты подписей | [notofonts.github.io](https://notofonts.github.io/) (глифы — [OpenFreeMap](https://openfreemap.org)) | SIL OFL 1.1 |
| MapLibre GL JS + pmtiles JS | отрисовка на фронте | [maplibre.org](https://maplibre.org/) | BSD-3-Clause |
| pgx, robfig/cron (Go) | драйвер БД, cron | [github.com/jackc/pgx](https://github.com/jackc/pgx), [github.com/robfig/cron](https://github.com/robfig/cron) | MIT |

Полный список с версиями и текстами — [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).

## Конфигурация

| Переменная | По умолчанию | Описание |
|---|---|---|
| `PBF_URL` | не задан | Откуда скачивать OSM PBF. Пусто — использовать только локальный `PBF_PATH` |
| `PBF_PATH` | `$DATA_DIR/osm/belarus-latest.osm.pbf` | Локальный файл PBF (кэш загрузки, офлайн-импорт) |
| `UPDATE_ON_START` | `false` | Обновлять данные при каждом старте контейнера |
| `UPDATE_SCHEDULE` | пусто (выключено) | Cron-расписание обновлений, например `0 4 * * 1` |
| `TILES_ENABLED` | `true` | Собирать карту из PBF (tilemaker) |
| `TILEMAKER_THREADS` | `0` (авто) | Потоков tilemaker при сборке тайлов |
| `TILEMAKER_CONFIG` / `TILEMAKER_PROCESS` | профиль OpenMapTiles из образа | Пути к конфигу слоёв и Lua-профилю |
| `ENABLE_EXAMPLE` | `true` | Отдавать страницу-пример `/example` (в проде можно выключить) |
| `IMPORT_PROCESSES` | число CPU | Процессов `osm2pgsql` при импорте |
| `DATA_DIR` | `/data` | Корень данных: БД, PBF, тайлы, состояние |
| `HTTP_ADDR` | `:8080` | Адрес API |
| `DATABASE_URL` / `PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`, `PGDATABASE` | встроенная БД | Подключение к PostgreSQL |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

## Разработка

```bash
go build ./...
go vet ./...
go test ./...
```

Локальная сборка образа (для проверок и e2e):

```bash
docker build -t aliakseikarpenka/slup-geo:dev .
```

- `scripts/e2e.ps1` — полный e2e на реальном PBF (импорт, эталонные кейсы, тайлы, персистентность).
- `scripts/smoke.sh` — быстрая проверка запущенного сервиса.
- `scripts/debug-point.sql` — отладка локации точки прямо в psql.

Структура: `cmd/slup-geo` (CLI `serve`/`update`/`version`), `internal/api` (HTTP, тайлы, `/example`),
`internal/geo` (модели, SQL, логика поиска), `internal/update` (PBF, osm2pgsql, tilemaker),
`internal/schema` (SQL-схема), `tiles/tilemaker` (профиль), `tiles/web` (стиль, шрифты, пример),
`docker/` (entrypoint).

## CI/CD

- Pull request и push в `master`: `go build` / `go vet` / `go test`.
- Push в `master`: автотег (patch-бамп от последнего `v*`) и публикация образа в Docker Hub:
  `aliakseikarpenka/slup-geo:<версия>` и `:latest`.

## Лицензия

[MIT](LICENSE) © Aleksei Karpenko. Атрибуция карты: `© OpenMapTiles · © OpenStreetMap contributors`.
