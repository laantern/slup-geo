# slup-geo

**Гео-апплаенс для продуктов на OpenStreetMap.** Один контейнер: PostgreSQL + PostGIS, импорт OSM,
поиск адресов и готовая карта для фронтенда — без внешних гео-API, ключей и лимитов.

[![build](https://github.com/laantern/slup-geo/actions/workflows/build.yml/badge.svg)](https://github.com/laantern/slup-geo/actions/workflows/build.yml)

Снаружи — образ, один volume и ENV. Внутри — два равноценных блока поверх общего движка данных:
**API для бэкенда** и **карта для фронтенда**. Архитектуры: `linux/amd64`, `linux/arm64`.

## Что решает

| Задача продукта | Блок | Ответ сервиса |
|---|---|---|
| Подсказать адрес при вводе | **API** | `GET /v1/suggest?q=Советская` — дома, улицы, районы |
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
    stop_grace_period: 2m          # обновление/остановка успевают завершиться аккуратно
    read_only: true                # образ пишет только в /data и tmpfs
    tmpfs:
      - /tmp
      - /var/run/postgresql
    security_opt:
      - no-new-privileges:true
    cap_drop: [ALL]
    cap_add: [CHOWN, DAC_OVERRIDE, FOWNER, SETGID, SETUID, KILL]
    # ports:
    #   - "8080:8080"   # только если обращаетесь с хоста; в docker-сети — http://geo:8080
    environment:
      # ОБЯЗАТЕЛЬНОЕ, если нет локального PBF
      PBF_URL: https://download.geofabrik.de/europe/belarus-latest.osm.pbf

      # НЕОБЯЗАТЕЛЬНЫЕ (значения — по умолчанию)
      # PBF_PATH: /data/osm/belarus-latest.osm.pbf   # локальный PBF: кэш/офлайн-импорт
      # UPDATE_ON_START: "false"                     # обновлять данные при каждом старте
      # UPDATE_SCHEDULE: "0 4 * * 1"                 # cron обновлений (пусто = выключено)
      # TZ: Europe/Minsk                             # таймзона контейнера (по умолчанию UTC)
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

Полный пример с hardening — [deploy/compose.example.yml](deploy/compose.example.yml).

Первый запуск скачивает PBF, импортирует его и собирает тайлы — минуты (для Беларуси ~5–8 минут).
Пока идёт импорт, сервис ещё не слушает порт. Готовность — `{"status":"ok"}`:

```bash
curl http://localhost:8080/health
# {"status":"ok","schemaReady":true,"importedAt":"...","lastUpdate":{"state":"ok",...}}
```

Если данных ещё нет, `/health` честно отвечает `503 {"status":"starting"|"no_data"}` — healthcheck
не бывает зелёным при пустой базе. После старта откройте **http://localhost:8080/example** —
готовая страница с картой и поиском.

## Блок 1 — API: поиск и гео-данные

Для вашего бэкенда: подсказки, локация точки, границы. Вызывается по внутренней сети
(`http://geo:8080`); публичный контракт, auth, CORS и лимиты остаются на вашей стороне.
**Наружу порт публиковать не нужно** — это внутренний API.

| Метод и путь | Что отдаёт |
|---|---|
| `GET /v1/suggest?q` | До 10 подсказок: админ-зоны/улицы → дома улицы → зоны улицы → зоны по имени. `q` — 2…128 символов |
| `GET /v1/point?lat&lon` | Дом (≤5 м от контура) + зоны точки, подпись `text` |
| `GET /v1/areas/{id}?simplify=display\|none` | Границы зоны или контур дома (GeoJSON). `simplify` — только `display`/`none` |

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

Особенности:

- `geometry` в `/v1/areas` — `Polygon`/`MultiPolygon` для зон, `Point` для адресов-нод (`N…`);
  для зданий (`W…`) — `Polygon`.
- Ошибки — Problem Details, машинный код в `type`: `VALIDATION_ERROR`, `NOT_FOUND`,
  `INTERNAL_SERVER_ERROR`. Полная спецификация — [openapi.yaml](openapi.yaml).
- Таймауты запросов к БД ограничены (15 с), у сервера — общие HTTP-таймауты.

## Блок 2 — Карта: всё для фронтенда

Равноценный API блок: чтобы нарисовать карту, внешние сервисы не нужны — сервис отдаёт данные,
стиль и шрифты, а рисует браузер (MapLibre GL JS + плагин pmtiles).

| Что | URL | Формат |
|---|---|---|
| Стиль (OSM Bright под схему OpenMapTiles) | `/tiles/style.json` | MapLibre Style JSON |
| Данные карты (версионные, `immutable`) | `/tiles/basemap-*.pmtiles` | PMTiles (внутри MVT) |
| Стабильный адрес актуального архива | `/tiles/basemap.pmtiles` | PMTiles |
| Подписи (глифы Noto) | `/tiles/fonts/...` | pbf |
| Вендорные библиотеки (MapLibre ESM, pmtiles) | `/tiles/vendor/...` | js/css |
| Готовая страница-пример | `/example` | HTML |

Подключение — две строки:

```js
maplibregl.addProtocol('pmtiles', new pmtiles.Protocol().tile);
const map = new maplibregl.Map({ container: 'map', style: '/tiles/style.json' });
```

- Пути в стиле абсолютные (`/tiles/...`): работает за любым доменом, nginx и CDN **без настроек**.
  При проксировании **под подпутём** (`/geo/tiles/...`) поправьте пути в `style.json` или отдавайте
  сервис в корне.
- Подписи — на русском (`name:ru`), при отсутствии — исходное имя из OSM.
- Без POI-иконок: базовый стиль статический; иконки можно добавить своим спрайтом с абсолютным URL.
- Атрибуция обязательна: `© OpenMapTiles · © OpenStreetMap contributors`.
- Библиотеки вендорные, без CDN: берите их из `/tiles/vendor/` (доступны и при `ENABLE_EXAMPLE=false`).
- Выключается: `TILES_ENABLED=false` (API продолжает работать), страница — `ENABLE_EXAMPLE=false`.

## Блок 3 — Движок данных

```
[OSM PBF] → osm2pgsql (staging) → atomic swap → PostgreSQL+PostGIS → geo.* → поисковый API
          ↘ tilemaker → basemap-*.pmtiles → /tiles/* → карта на фронте
```

**`slup-geo update`** (вручную, при старте `UPDATE_ON_START=true` или по cron `UPDATE_SCHEDULE`):

1. скачивание PBF (условный `If-Modified-Since`, ретраи; при сбое сети — работа на локальном кэше),
   проверка заголовка PBF до импорта;
2. `tilemaker` собирает тайлы из того же PBF во временный файл;
3. `osm2pgsql` импортирует PBF в **staging-таблицы** `planet_osm_next_*` — рабочие данные и API
   не трогаются;
4. одной транзакцией: старые таблицы заменяются на staging, пересобираются представления
   `geo.zones/streets/addresses/names` (склейка мультиполигонов, поисковый индекс имён),
   индексы GIST/`pg_trgm`, `ANALYZE`. При любой ошибке транзакция откатывается — работающие
   данные остаются на месте;
5. публикация тайлов (`basemap-<время>.pmtiles` + `tiles.json`) и маркеров состояния
   (`state.json`, `status.json`) — атомарно (temp + fsync + rename).

Сбой обновления (битый PBF, кончился диск, OOM) не оставляет сервис без данных: старые таблицы
и тайлы продолжают работать, ошибка видна в `/status` и в логах.

**Ориентиры** для Беларуси (PBF 334 МБ): БД ~4.2 ГБ, тайлы ~380 МБ, сборка тайлов ~40 с на 16
ядрах. На время обновления нужно **~2× места под БД** (старые и staging-таблицы сосуществуют).
**Данные** — один volume `geo_data`: БД, PBF-кэш, тайлы, состояние; пересоздание контейнера данные
не теряет.

Ручное обновление в контейнере:

```bash
docker compose exec -u slup geo slup-geo update
```

Расписание — 5-полевой cron (`minute hour day month weekday`), время локальное для контейнера
(по умолчанию UTC; задайте `TZ`, например `TZ=Europe/Minsk`). Невалидное расписание не роняет
сервис — пишется ошибка, cron выключается.

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
| MapLibre GL JS 6 + pmtiles JS | отрисовка на фронте | [maplibre.org](https://maplibre.org/) | BSD-3-Clause |
| pgx, robfig/cron (Go) | драйвер БД, cron | [github.com/jackc/pgx](https://github.com/jackc/pgx), [github.com/robfig/cron](https://github.com/robfig/cron) | MIT |

Полный список с версиями и текстами — [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).

## Наблюдаемость

| Ручка | Что показывает |
|---|---|
| `GET /health` | `ok` (данные готовы) / `starting` / `no_data` / `unavailable` + `importedAt`, `lastUpdate` |
| `GET /status` | версия, `schemaReady`, данные (`state.json`), последнее обновление (`status.json`) |

Логи — stdout контейнера: `serve`/`update` пишут этапы обновления; HTTP-запросы — access-лог
(метод, путь, статус, длительность; query не логируется — там бывают адреса). `LOG_LEVEL=debug`
добавляет healthcheck/тайлы.

## Конфигурация

| Переменная | По умолчанию | Описание |
|---|---|---|
| `PBF_URL` | не задан | Откуда скачивать OSM PBF (только `https://`; `http` — для локальных зеркал). Пусто — использовать только локальный `PBF_PATH` |
| `PBF_PATH` | `$DATA_DIR/osm/belarus-latest.osm.pbf` | Локальный файл PBF (кэш загрузки, офлайн-импорт) |
| `UPDATE_ON_START` | `false` | Обновлять данные при каждом старте контейнера (`1/true/yes/on`) |
| `UPDATE_SCHEDULE` | пусто (выключено) | 5-полевой cron обновлений, например `0 4 * * 1`; время локальное (`TZ`, по умолчанию UTC) |
| `TILES_ENABLED` | `true` | Собирать карту из PBF (tilemaker) |
| `TILEMAKER_THREADS` | `0` (авто) | Потоков tilemaker при сборке тайлов (0–256) |
| `TILEMAKER_CONFIG` / `TILEMAKER_PROCESS` | профиль OpenMapTiles из образа | Пути к конфигу слоёв и Lua-профилю |
| `ENABLE_EXAMPLE` | `true` | Отдавать страницу-пример `/example` (в проде можно выключить) |
| `WEB_DIR` | `/usr/local/share/slup-geo` | Каталог веб-ассетов (стиль, глифы, вендорные библиотеки) |
| `IMPORT_PROCESSES` | число CPU | Процессов `osm2pgsql` при импорте (1–256) |
| `SHUTDOWN_TIMEOUT_SECONDS` | `15` | Сколько ждать аккуратной остановки HTTP-сервера (1–3600) |
| `DATA_DIR` | `/data` | Корень данных: БД, PBF, тайлы, состояние |
| `HTTP_ADDR` | `:8080` | Адрес API |
| `DATABASE_URL` / `PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`, `PGDATABASE` | встроенная БД | Подключение к PostgreSQL (при `DATABASE_URL` апплаенс не управляет ролями) |
| `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD` | `geo_db`, `geo_user`, генерируется | Параметры встроенной БД (пароль хранится в volume) |
| `TZ` | UTC | Таймзона контейнера (влияет на cron) |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

Невалидные числовые значения — ошибка при старте с понятным сообщением (без тихих дефолтов).

## Развёртывание за nginx

Публично — только тайлы и, при желании, `/example`; API остаётся во внутренней сети:

```nginx
location /tiles/ {
    proxy_pass http://geo:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}
# при необходимости:
# location /example { proxy_pass http://geo:8080; }
```

Для защиты внутреннего API от злоупотреблений, если он всё же доступен извне, используйте
`limit_req` на `/v1/` и/или network policy — сервис не содержит встроенного rate-limit
(за обратным прокси все запросы идут с одного IP, что делает его бессмысленным).

## Обновление и совместимость

- Образ тегируется `vX.Y.Z` и `latest`; в продакшене пиньте версию/дижест, а не `latest`.
- Версия схемы данных (`geo.meta.schema_version`) проверяется при старте: несовпадение —
  предупреждение в логе, лечится `slup-geo update`.
- Изменения по версиям — [CHANGELOG.md](CHANGELOG.md). В `v0.1.1` удалены неиспользуемые
  ENV (`TILES_URL`, `TILES_BBOX`, `TILES_MAXZOOM`) и корневой compose-файл.

## FAQ

- **Чем отличается от Nominatim/Photon/Pelias?** Один контейнер вместо стека; поиск заточен под
  «улица → дома» (префиксы, опечатки, «14 Полесская» = «Полесская 14»), локация точки с иерархией
  зон и карта (PMTiles + стиль + шрифты) в той же коробке. Nominatim — полноценный геокодер
  с другими задачами (POI, структурированные запросы).
- **Работает офлайн?** Да: положите PBF в volume и не задавайте `PBF_URL`; библиотеки вендорные,
  CDN не нужны.
- **Можно ли другой регион?** Да, любой PBF OSM: укажите свой `PBF_URL`/`PBF_PATH`.
- **Нужны ли бэкапы?** Нет: данные воспроизводимы из PBF; храните только volume (кэш ускоряет
  перезапуск).
- **Есть ли ключи и лимиты?** Нет внешних сервисов — ключи не нужны; лимиты — ваша зона
  ответственности (прокси/сеть).
- **Что с лицензиями?** Код — MIT; данные OSM — ODbL; обязательна атрибуция
  `© OpenMapTiles · © OpenStreetMap contributors` (см. [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md)).

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
- `deploy/compose.example.yml` — пример compose с hardening.

Структура: `cmd/slup-geo` (CLI `serve`/`update`/`version`), `internal/api` (HTTP, тайлы, `/example`),
`internal/geo` (модели, SQL, логика поиска), `internal/update` (PBF, osm2pgsql, tilemaker),
`internal/schema` (SQL-схема), `tiles/tilemaker` (профиль), `tiles/web` (стиль, шрифты, вендорные
библиотеки, пример), `docker/` (entrypoint).

## CI/CD

- Pull request и push в `master`: `go build` / `go vet` / `go test` / `govulncheck`; SQL-схема
  проверяется на живом PostGIS.
- Push в `master`: автотег (patch-бамп от последнего `v*`) и публикация образа в Docker Hub
  (`linux/amd64`, `linux/arm64`) с SBOM и provenance: `aliakseikarpenka/slup-geo:<версия>` и `:latest`.
- Зависимости и базовые образы обновляет Dependabot.

## Лицензия

[MIT](LICENSE) © Aleksei Karpenko. Атрибуция карты: `© OpenMapTiles · © OpenStreetMap contributors`.
