# slup-geo

**Гео-апплаенс: OSM-данные, поиск адресов, зоны и карта — в одном контейнере.**

[![build](https://github.com/laantern/slup-geo/actions/workflows/build.yml/badge.svg)](https://github.com/laantern/slup-geo/actions/workflows/build.yml)

`slup-geo` — самодостаточный сервис для задач «где находится эта точка» и «подскажи адрес».
Внутри одного образа: PostgreSQL + PostGIS, импорт OSM (`osm2pgsql`), поисковый API и векторные
тайлы PMTiles. Снаружи — только образ, один volume и переменные окружения: база, креды и утилиты
не администрируются снаружи.

## Возможности

- **Подсказки адресов** (`/v1/suggest`) — улицы, города, районы и дома; разбор «улица + номер дома»
  («14 Полесская» = «Полесская 14»), нечёткий поиск по триграммам, ранжирование с учётом важности города.
- **Локация точки** (`/v1/point`) — дом в пределах 5 метров от контура и административные зоны
  по возрастанию площади; готовая подпись вида «улица Григория Денисенко, д. 22, Гомель».
- **Границы** (`/v1/areas/{id}`) — GeoJSON, упрощённая (по умолчанию) или точная геометрия;
  мультиполигоны и эксклавы районов сохраняются.
- **Карта** (`/tiles/*`) — векторная подложка PMTiles, собранная из того же PBF (tilemaker,
  схема OpenMapTiles); Range-запросы, версионные файлы.
- **Автономные обновления** — скачивание PBF, импорт и пересборка данных/тайлов по расписанию
  (`UPDATE_SCHEDULE`), при старте (`UPDATE_ON_START`) или вручную.
- **Данные воспроизводимы из PBF** — резервные копии не критичны, пересоздание занимает минуты.

## Быстрый старт

Пример со всеми переменными: обязательное отмечено, остальное — значения по умолчанию.

```yaml
services:
  geo:
    image: aliakseikarpenka/slup-geo:latest
    restart: unless-stopped
    environment:
      # ОБЯЗАТЕЛЬНОЕ, если нет локального PBF
      PBF_URL: https://download.geofabrik.de/europe/belarus-latest.osm.pbf

      # НЕОБЯЗАТЕЛЬНЫЕ (значения — по умолчанию)
      # PBF_PATH: /data/osm/belarus-latest.osm.pbf   # локальный PBF: кэш/офлайн-импорт
      # UPDATE_ON_START: "false"                     # обновлять данные при каждом старте
      # UPDATE_SCHEDULE: "0 4 * * 1"                 # cron обновлений (пусто = выключено)
      # TILES_ENABLED: "true"                        # собирать подложку из PBF (tilemaker)
      # TILEMAKER_THREADS: "0"                       # потоков tilemaker (0 = авто)
      # IMPORT_PROCESSES: "4"                        # процессов osm2pgsql
      # DATA_DIR: /data                              # БД, PBF, тайлы, состояние
      # HTTP_ADDR: ":8080"
      # LOG_LEVEL: info                              # debug | info | warn | error
    volumes:
      - geo_data:/data

volumes:
  geo_data:
```

### Попробовать

```bash
curl 'http://localhost:8083/v1/suggest?q=Бородина'
curl 'http://localhost:8083/v1/point?lat=52.3955063&lon=30.9607992'
curl 'http://localhost:8083/v1/areas/W-3628812?simplify=display'
curl 'http://localhost:8083/tiles/tiles.json'
```

Пример ответа `GET /v1/point?lat=52.3955063&lon=30.9607992`:

```json
{
  "text": "улица Григория Денисенко, д. 22, Гомель",
  "zones": [
    { "id": "W664945556", "name": "улица Григория Денисенко, д. 22", "level": "HOUSE", "kind": "BUILDING", "areaM2": 1460 },
    { "id": "W-1", "name": "Железнодорожный район", "level": "CITY_DISTRICT", "kind": "ADMIN", "areaM2": 36777954 }
  ]
}
```

## API

| Метод и путь | Назначение |
|---|---|
| `GET /v1/point?lat&lon` | Дом (≤5 м от контура) + зоны точки, подпись `text` |
| `GET /v1/suggest?q` | До 10 подсказок: админ-зоны/улицы → дома улицы → зоны улицы → зоны по имени |
| `GET /v1/areas/{id}?simplify=display\|none` | Границы зоны или контур дома (GeoJSON) |
| `GET /tiles/tiles.json` | Манифест тайлов: актуальный версионный файл |
| `GET /tiles/<basemap-*.pmtiles>` | PMTiles-файл (Range, `immutable`) |
| `GET /health` | Живость сервиса и БД |

`/v1/*` — **внутренний API**: вызывайте его из своего backend по внутренней сети (в docker-сети —
`http://geo:8080`), наружу публикуйте только `/tiles/*` через reverse proxy. Ошибки — в формате
Problem Details с машинным кодом в `type` (`VALIDATION_ERROR`, `NOT_FOUND`, `INTERNAL_SERVER_ERROR`).
Полная спецификация — [openapi.yaml](openapi.yaml).

## Подключение карты (фронтенд)

Сервис отдаёт тайлы в формате **[PMTiles](https://github.com/protomaps/PMTiles)** — один
файл-архив; внутри — векторные тайлы **[Mapbox Vector Tile v2](https://github.com/mapbox/vector-tile-spec)**
в схеме **[OpenMapTiles](https://openmaptiles.org/schema/)**. Фронтенд рисует их библиотекой
**[MapLibre GL JS](https://maplibre.org/)** с плагином **pmtiles** и стилем по
**[MapLibre Style Specification](https://maplibre.org/maplibre-style-spec/)**.

| URL | Формат | Назначение |
|---|---|---|
| `/tiles/tiles.json` | JSON (манифест сервиса) | какой файл тайлов актуален |
| `/tiles/basemap-*.pmtiles` | PMTiles (внутри MVT, схема OpenMapTiles) | данные карты (Range-запросы) |
| `/tiles/style.json` | MapLibre Style JSON | готовый стиль под схему OpenMapTiles (следующий этап) |

Схема подключения (стиль — свой или готовый из сервиса, когда появится):

```js
import maplibregl from 'maplibre-gl';
import { Protocol } from 'pmtiles';

maplibregl.addProtocol('pmtiles', new Protocol().tile);

const map = new maplibregl.Map({
  container: 'map',
  style: '/tiles/style.json',
});
```

Атрибуция обязательна при показе карты: «© OpenStreetMap contributors».

## Конфигурация

| Переменная | По умолчанию | Описание |
|---|---|---|
| `PBF_URL` | не задан | Откуда скачивать OSM PBF. Пусто — использовать только локальный `PBF_PATH` |
| `PBF_PATH` | `$DATA_DIR/osm/belarus-latest.osm.pbf` | Локальный файл PBF (кэш загрузки, офлайн-импорт) |
| `UPDATE_ON_START` | `false` | Обновлять данные при каждом старте контейнера |
| `UPDATE_SCHEDULE` | пусто (выключено) | Cron-расписание обновлений, например `0 4 * * 1` |
| `TILES_ENABLED` | `true` | Собирать векторную подложку из PBF (tilemaker) |
| `TILEMAKER_THREADS` | `0` (авто) | Потоков tilemaker при сборке тайлов |
| `TILEMAKER_CONFIG` / `TILEMAKER_PROCESS` | профиль OpenMapTiles из образа | Пути к конфигу слоёв и Lua-профилю |
| `IMPORT_PROCESSES` | число CPU | Процессов `osm2pgsql` при импорте |
| `DATA_DIR` | `/data` | Корень данных: БД, PBF, тайлы, состояние |
| `HTTP_ADDR` | `:8080` | Адрес API |
| `DATABASE_URL` / `PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`, `PGDATABASE` | встроенная БД | Подключение к PostgreSQL |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

## Как это работает

**Обновление данных** (`slup-geo update`):

1. advisory lock — параллельные обновления не запускаются;
2. скачивание PBF (условный запрос `If-Modified-Since`, если файл уже есть);
3. `osm2pgsql --create --hstore-all --latlong` — таблицы `planet_osm_*`;
4. представления `geo.zones` (склейка мультиполигонов, упрощённая геометрия), `geo.streets`,
   `geo.addresses`, `geo.names` (поисковый индекс имён) + индексы (pg_trgm, GIST);
5. `tilemaker` — векторные тайлы из того же PBF (схема OpenMapTiles, зумы 0–14):
   версионный `basemap-<время>.pmtiles` (temp + rename) и `tiles.json` (атомарно);
6. маркер `state.json` (атомарно).

**Сервис** (`slup-geo serve`) отдаёт API и тайлы; при заданном `UPDATE_SCHEDULE` внутри процесса
работает cron, который повторяет цикл обновления.

**Данные** — один volume `geo_data`: PostgreSQL, PBF-кэш, тайлы и состояние. Пересоздание
контейнера данные не теряет; полная очистка — `docker compose down -v`.

### Подложка карты (тайлы)

Подложка собирается **из того же OSM PBF**, что и поисковые данные: `tilemaker` (профиль
OpenMapTiles) нарезает векторные тайлы (зумы 0–14) в один файл `basemap-<версия>.pmtiles`.
Внешних источников тайлов нет — только ваш PBF.

Ориентиры для Беларуси (PBF 334 МБ): архив ~370 МБ, сборка ~минуты (замер: 40 с на 16 ядрах),
пиковый промежуточный диск ~1 ГБ в `$DATA_DIR/tmp`.

- `TILES_ENABLED=false` — тайлы не пересобираются (API работает полностью, карта без подложки);
- файл отдаётся по `/tiles/*` (Range, версионные имена `immutable`), манифест `/tiles/tiles.json`
  указывает актуальный файл;
- для отрисовки на фронте нужен стиль под схему OpenMapTiles (готовый стиль и страница-пример —
  следующий этап);
- атрибуция OSM (`© OpenStreetMap contributors`) обязательна при показе карты.

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

- `scripts/e2e.ps1` — полный e2e на реальном PBF (импорт, эталонные кейсы, персистентность volume).
- `scripts/smoke.sh` — быстрая проверка запущенного сервиса.
- `scripts/debug-point.sql` — отладка локации точки прямо в psql.

Структура: `cmd/slup-geo` (CLI `serve`/`update`/`version`), `internal/api` (HTTP и тайлы),
`internal/geo` (модели, SQL, логика поиска), `internal/update` (PBF, osm2pgsql, tilemaker),
`internal/schema` (SQL-схема), `tiles/` (профиль tilemaker), `docker/` (entrypoint).

Сторонние компоненты и лицензии — [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).

## CI/CD

- Pull request и push в `master`: `go build` / `go vet` / `go test`.
- Push в `master`: автотег (patch-бамп от последнего `v*`) и публикация образа в Docker Hub:
  `aliakseikarpenka/slup-geo:<версия>` и `:latest`.

## Лицензия

[MIT](LICENSE) © Aleksei Karpenko
