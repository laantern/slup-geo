# AGENTS.md — правила работы в репозитории slup-geo

## Что это

Гео-апплаенс в одном Docker-образе: PostgreSQL + PostGIS + osm2pgsql + tilemaker + Go-сервис.
Отдаёт внутренний API (`/v1/point|suggest|areas`), тайлы/стиль/шрифты (`/tiles/*`) и страницу-пример
(`/example`). Язык кода, комментариев, документации и коммитов — русский (как в проекте).

## Команды

```bash
go build ./... && go vet ./... && go test ./...          # обычные проверки
docker build -t aliakseikarpenka/slup-geo:dev .          # образ для e2e

# интеграционные тесты SQL/PGStore и swap на живом PostGIS (пакеты — последовательно, -p 1):
docker run -d --name pgtest -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=geo_test -p 5433:5432 postgis/postgis:16-3.4
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5433/geo_test?sslmode=disable' \
  go test -p 1 -tags=integration ./internal/geo/... ./internal/update/...

# полный e2e на реальном PBF (Windows PowerShell):
powershell -File scripts/e2e.ps1 -PbfPath D:\osm\belarus-latest.osm.pbf
```

## Инварианты (не нарушать)

1. **`/v1/*` — внутренний API без аутентификации.** Не публиковать порт наружу; наружу — только
   `/tiles/*`. Rate-limit — забота потребителя (nginx/сеть).
2. **Обновление неразрушающее.** Импорт — только в staging-таблицы `planet_osm_next_*`; матвью
   собираются заранее под временными именами `geo.*_next` **вне транзакции**
   (`buildViewsStaging`), финальная замена — короткая метаданная транзакция только с `DROP`/`RENAME`
   (`swapPlanetTables`). Нельзя добавлять в финальную транзакцию пересборку данных и нельзя
   трогать рабочие таблицы до успешного импорта. Сбой любого шага обязан оставлять прежние
   данные и матвью работающими.
3. **Роли БД.** `update` работает ролью `geo_owner` (без суперправ), `serve` — `geo_reader`
   (только SELECT). `geo_user` — администратор образа (entrypoint), приложению его пароль
   не передаётся (`env -u POSTGRES_PASSWORD`, файл `db_password` — root-only). PostgreSQL
   слушает только `127.0.0.1`, локальные подключения требуют пароль (scram).
4. **Секреты.** Пароли ролей — в `/data/state/db_password*` (0600). Не логировать `PBF_URL`
   с учётными данными (использовать `redactURL`); не коммитить `.env`, PBF, pmtiles.
5. **Данные PBF.** Скачивание только `https` (http — локальные зеркала), с проверкой заголовка
   PBF до импорта и лимитом размера. `PBF_URL`/`PBF_PATH` — единственные входы данных.
6. **Атрибуция.** Карта требует `© OpenMapTiles · © OpenStreetMap contributors` (в `/example`
   и в стиле/метаданных тайлов). Лицензии зависимостей — в THIRD_PARTY_LICENSES.md.
7. **Supply chain.** Базовые образы Dockerfile — по digest, Actions — по commit SHA,
   зависимости Go обновляет Dependabot. Новые зависимости — только с совместимой лицензией
   и записью в THIRD_PARTY_LICENSES.md.
8. **Документация синхронна коду.** При изменении ENV/ручек/поведения обновлять README,
   openapi.yaml и CHANGELOG.md. Версия схемы данных — `internal/schema.Version`.

## Структура

- `cmd/slup-geo` — CLI: `serve`, `update`, `version` (fallback паролей для `docker exec` — в main.go).
- `internal/api` — HTTP: ручки, тайлы, `/example`, middleware (заголовки, access-лог).
- `internal/geo` — домен: модели, `pgstore.go` (SQL), сервисы point/suggest/areas.
- `internal/update` — PBF (скачивание/валидация), osm2pgsql (staging), swap+views, tilemaker, статусы.
- `internal/schema` — SQL-схема (embed): extensions, views, names, geo.meta.
- `tiles/tilemaker` — профиль OpenMapTiles; `tiles/web` — стиль, глифы, вендорные библиотеки, пример.
- `docker/entrypoint.sh` — жизненный цикл: PostgreSQL, роли, update, serve, коды выхода.

## Проверки перед коммитом

- `go build ./...`, `go vet ./...`, `go test ./...`, `govulncheck ./...`.
- Изменения SQL/схемы — интеграционный тест (см. команды выше).
- Изменения тайлов/стиля/`/example` — e2e и проверка страницы в браузере (Playwright):
  карта рендерится, поиск/точка/границы работают, консоль без ошибок, атрибуция на месте.
- Изменения update/entrypoint — e2e с существующим и чистым volume (данные переживают рестарт,
  сбой обновления не роняет API).
