# slup-geo

Гео-апплаенс Slup: **один контейнер** с PostgreSQL + PostGIS, Go-приложением и
инструментами импорта OSM/сборки тайлов. Модель — как у Nominatim: наружу торчат
только образ, один volume `geo_data` и ENV; БД, креды и утилиты внутри не администрируются.

Решение и контекст — `../Slup/docs/adr/0004-geo-util-service.md`.

## Что внутри

| Компонент | Роль |
|---|---|
| PostgreSQL + PostGIS | `planet_osm_*` (импорт osm2pgsql) и представления `geo.zones/streets/addresses/names` |
| `slup-geo serve` | внутренний API (`/v1/point`, `/v1/suggest`, `/v1/areas/{id}`), тайлы `/tiles/*`, `/health`; cron обновлений при `UPDATE_SCHEDULE` |
| `slup-geo update` | скачать PBF → импорт osm2pgsql → пересобрать представления → собрать PMTiles → маркер состояния |
| entrypoint | старт PostgreSQL, первый импорт при пустой БД/`UPDATE_ON_START`, запуск `serve` |

Внутренние API-ручки наружу не выставляются: их вызывает монолит по docker-сети.
Через nginx публикуются только `/tiles/*` (Cloudflare кэширует сверху).

## Запуск

```powershell
docker compose up --build
# первый старт: скачивание PBF (~334 МБ) + импорт — несколько минут
curl.exe "http://localhost:8083/health"
curl.exe "http://localhost:8083/v1/point?lat=52.3955063&lon=30.9607992"
```

Остановка — `docker compose down` (данные остаются в volume `geo_data`; полная очистка — `down -v`).

## ENV

| Переменная | По умолчанию | Смысл |
|---|---|---|
| `PBF_URL` | не задан | Откуда скачивать PBF (пусто — только локальный `PBF_PATH`; в `docker-compose.yml` подставлен Geofabrik Belarus) |
| `PBF_PATH` | `$DATA_DIR/osm/belarus-latest.osm.pbf` | Куда скачивать/откуда читать PBF (кэш проверяется через `If-Modified-Since`) |
| `UPDATE_ON_START` | `false` | Обновлять данные при каждом старте контейнера |
| `UPDATE_SCHEDULE` | пусто (выключено) | Cron обновлений внутри сервиса, например `0 4 * * 1` |
| `TILES_URL` | пусто | Источник PMTiles для `pmtiles extract` (пусто — тайлы не пересобираются) |
| `TILES_BBOX` | `23.1,51.2,32.8,56.2` | Область выборки тайлов: minLon,minLat,maxLon,maxLat |
| `TILES_MINZOOM` / `TILES_MAXZOOM` | `0` / `15` | Диапазон зумов тайлов |
| `IMPORT_PROCESSES` | число CPU | Процессов osm2pgsql |
| `DATA_DIR` | `/data` | Корень данных: БД (`pgdata`), PBF, тайлы, состояние |
| `HTTP_ADDR` | `:8080` | Адрес внутреннего API |
| `DATABASE_URL` / `PG*` | `geo_db`/`geo_user`/… | Подключение к БД (при штатном запуске — встроенной) |
| `LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |

## Обновление данных

При старте: если `state.json` отсутствует или `UPDATE_ON_START=true`, выполняется полный цикл.
Периодические обновления — `UPDATE_SCHEDULE` (cron, 5 полей); во время апдейта держится
advisory lock, второй апдейт не стартует. Ручной запуск в контейнере:

```powershell
docker compose exec geo slup-geo update
```

Порядок обновления: расширения → скачивание PBF → `osm2pgsql --create` → представления
(`geo.*`, матвью с уникальными индексами) → `ANALYZE` → `pmtiles extract` → `tiles.json`
(атомарно) → `state.json` (атомарно).

## Тайлы

- `GET /tiles/tiles.json` — манифест (no-cache), указывает актуальный `basemap-<версия>.pmtiles`.
- `GET /tiles/<файл>.pmtiles` — версионный файл (Range, `immutable`, whitelist имён, без gzip).
- Сборка — `pmtiles extract` из Protomaps build (bbox Беларуси, ≤ z15). Источник задаётся
  `TILES_URL`; при пустом значении тайлы не пересобираются, отдаётся прежний манифест.

## Разработка

```powershell
go build ./...
go vet ./...
go test ./...
go run ./cmd/slup-geo version
```

Дымовой тест по запущенному контейнеру:

```powershell
docker compose up -d --build
./scripts/smoke.sh http://localhost:8083
# или полный e2e с реальным PBF (импорт + эталонные кейсы + персистентность):
./scripts/e2e.ps1 -PbfPath D:\osm\belarus-latest.osm.pbf
```

Страница проверки локации (MapLibre + PMTiles, ходит в монолит и тайлы гео) —
`../Slup/tools/location-page/`.

Эталонные кейсы (совпадают с проверками монолита):

- `52.3955063, 30.9607992` → «улица Григория Денисенко, д. 22, Гомель»;
- `suggest=Бородина` → дома улицы Бородина в Гомеле; `suggest=3я линейная` → дома 3-й Линейной;
- `suggest=Гомель` → город первым; `suggest=Железнодорожный` → сначала район;
- `areas: W664945556` и relation-зоны (`W-…`) отдают MultiPolygon с эксклавами.

## Публикация образа

GitHub Actions (`.github/workflows/build.yml`):

- на PR и на push в `master` — job `test` (`go build`/`go vet`/`go test`);
- на push в `master` — автотег (patch-бамп от последнего тега `v*`; повторный запуск на том же
  коммите переиспользует уже созданный тег) и публикация в Docker Hub:
  `<DOCKERHUB_USERNAME>/slup-geo:<версия>` и `:latest`.

Секреты репозитория (Settings → Secrets and variables → Actions): `DOCKERHUB_USERNAME` (логин)
и `DOCKERHUB_TOKEN` (access token с правами Read & Write). Изменения только в документации
(`**/*.md`, `docs/**`) пайплайн не запускают.

Первый образ: после добавления секретов любой мерж в `master` создаст тег `v0.1.0` и опубликует
образ. На сервере — `docker compose pull && docker compose up -d` (для приватного репозитория
Docker Hub — предварительный `docker login`).
