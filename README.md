# slup-geo

**Гео-апплаенс для продуктов на OpenStreetMap: поиск адресов, «где эта точка» и карта-подложка —
из одного PBF, одним контейнером, без внешних гео-API.**

[![build](https://github.com/laantern/slup-geo/actions/workflows/build.yml/badge.svg)](https://github.com/laantern/slup-geo/actions/workflows/build.yml)

`slup-geo` закрывает три типовые задачи продукта: подсказать адрес при вводе, определить, где
находится точка (дом и административные зоны), и нарисовать карту. Внутри одного образа —
PostgreSQL + PostGIS, импорт OSM (`osm2pgsql`), поисковый API и сборка векторных тайлов
(`tilemaker` → PMTiles). Снаружи — только образ, один volume и переменные окружения.

## Зачем использовать

- **Убрать внешние гео-API из продукта.** Ни ключей, ни абонплаты, ни лимитов, ни отправки
  адресов пользователей третьим лицам. Данные и вычисления — внутри вашего контура.
- **Поиск, заточенный под адреса.** «Улица → дома»,
  префиксы и опечатки (триграммы), приоритет важного города, разбор «14 Полесская» = «Полесская 14».
- **Карта из своих данных.** Векторные тайлы PMTiles из того же PBF: любой стиль — светлая/тёмная
  тема, язык подписей, брендовые цвета — **без пересборки тайлов**.
- **Эксплуатация как апплаенс.** Один образ, один volume, ENV. База, креды и утилиты внутри;
  данные воспроизводимы из PBF — бэкапы не критичны, пересоздание занимает минуты.
- **Открытые форматы без lock-in.** PMTiles, MVT, схема OpenMapTiles, стиль MapLibre — всё
  стандартное, замена любого слоя не требует переписывать продукт.

## Когда не подходит

- Нужна **глобальная карта «из коробки»** — сюда надо подавать PBF своего региона (или вырезку).
- Нужна **маршрутизация** (построение маршрутов) — это другой класс сервисов.
- Нужны **растровые тайлы** (PNG) — здесь векторные; растр потребует серверного рендеринга.
- Нужна **минутная свежесть** OSM — обновления полным реимпортом по расписанию, инкрементальных
  диффов нет.
- Нужен **SaaS без эксплуатации** — это self-hosted сервис, хост и диск ваши.

## Гибкость: что можно менять

| Область | Что гибко |
|---|---|
| Регион и данные | Любой OSM PBF: страна, вырезка, свой URL или локальный файл (`PBF_URL` / `PBF_PATH`) |
| Поиск | Логика и SQL внутри сервиса — правила ранжирования, канонизация, состав ответа расширяемы; публичный контракт стабилен |
| Карта и стиль | Векторные тайлы + открытая схема → любой стиль, темы, языки; смена стиля не трогает тайлы |
| Отдача тайлов | PMTiles по Range: напрямую, через nginx, за Cloudflare — кэшируется как статика |
| Развёртывание | Контейнер + volume + ENV; API по внутренней docker-сети, наружу — только тайлы |
| Обновления | Вручную, при старте (`UPDATE_ON_START`) или по cron (`UPDATE_SCHEDULE`); под advisory lock |
| Язык данных | Подписи и поиск используют `name`, `name:ru`, `name:be` из OSM; запросы канонизируются |

## Границы ответственности

| Область | Сервис делает | Вы делаете сами |
|---|---|---|
| Публичный API | Внутренние ручки `/v1/point`, `/v1/suggest`, `/v1/areas` | Публичный контракт своего продукта: auth, CORS, лимиты, кэш — на вашем бэке (или reverse proxy) |
| Карта | Отдаёт данные PMTiles, манифест `tiles.json` и готовый стиль `/tiles/style.json` | Подключаете MapLibre + pmtiles, делаете UX карты; атрибуция «© OpenStreetMap contributors» обязательна |
| Стиль | Готовый стиль (OSM Bright под схему OpenMapTiles), спрайт и глифы — из образа; относительные ссылки работают за любым прокси | Кастомизация под бренд, тёмная тема, свои слои (MapLibre Style Spec) |
| Данные | Скачивает PBF, импортирует, строит представления и тайлы | Выбираете регион/PBF, расписание, следите за диском и логами |
| Свежесть | Полный реимпорт по расписанию | Определяете приемлемую частоту (диффов нет — это полный реимпорт) |
| Инфраструктура | Внутренняя БД, креды, утилиты — всё в контейнере | Хост, volume, прокси/CDN; бэкапы не нужны — данные воспроизводимы из PBF |

## Быстрый старт

Создайте `docker-compose.yml` из блока ниже и запустите:

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

Первый запуск скачивает PBF, импортирует его и собирает тайлы — несколько минут. Пока идёт импорт,
сервис ещё не слушает порт; готовность — `{"status":"ok"}`:

```bash
curl http://localhost:8080/health
```

### Попробовать

Если порт опубликован (`ports: ["8080:8080"]`), с хоста:

```bash
curl 'http://localhost:8080/v1/suggest?q=Бородина'
curl 'http://localhost:8080/v1/point?lat=52.3955063&lon=30.9607992'
curl 'http://localhost:8080/v1/areas/W-3628812?simplify=display'
curl 'http://localhost:8080/tiles/tiles.json'
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
| `/tiles/basemap.pmtiles` | PMTiles (внутри MVT, схема OpenMapTiles) | стабильный адрес актуального архива (no-cache) |
| `/tiles/basemap-*.pmtiles` | PMTiles | версионные файлы (immutable, для CDN) |
| `/tiles/style.json` | MapLibre Style JSON | готовый стиль под схему OpenMapTiles (ссылки подставляются сервисом) |
| `/tiles/sprite*`, `/tiles/fonts/...` | спрайт и глифы Noto | иконки и шрифты для стиля |

В стиле все ссылки **относительные** (`/tiles/...`): он работает за любым доменом, nginx и CDN
без единой настройки — при условии, что страница и `/tiles/*` на одном origin (обычная схема,
когда nginx отдаёт SPA и проксирует `/tiles`).

Схема подключения:

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
  указывает актуальный файл, стабильный алиас — `/tiles/basemap.pmtiles`;
- для отрисовки на фронте сервис отдаёт готовый стиль `/tiles/style.json` (+ спрайт и глифы);
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

- `scripts/e2e.ps1` — полный e2e на реальном PBF (импорт, эталонные кейсы, тайлы, персистентность volume).
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
