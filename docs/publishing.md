# Публикация и видимость

Готовые тексты для метаданных GitHub и Docker Hub, а также чек-лист «чтобы находили люди и ИИ».

## GitHub → About

**Description** (вставить как есть):

> Self-hosted OSM geocoder & map tiles: address search, point location, PMTiles — one Docker container, no external APIs

**Topics** (20):

```
openstreetmap, osm, geocoding, geocoder, reverse-geocoding, self-hosted, postgis,
pmtiles, vector-tiles, openmaptiles, tilemaker, nominatim-alternative, docker,
golang, maps, address-search, offline, privacy, gis, maplibre
```

**Website:** ссылка на README-раздел «Быстрый старт» или на будущий docs-сайт.
**Social preview:** скриншот страницы `/example` с картой (Settings → Social preview).

## Docker Hub → Description

**Short description:**

> Self-hosted OSM geocoder & map tiles in one container: address search, point location, PMTiles. No external APIs.

**Full description:** вставить markdown ниже (копия README, сокращённая). Не забыть указать
**Source repository** → `https://github.com/laantern/slup-geo`.

```markdown
# slup-geo

**Гео-апплаенс для продуктов на OpenStreetMap.** Один контейнер: PostgreSQL + PostGIS, импорт OSM,
поиск адресов и готовая карта для фронтенда — без внешних гео-API, ключей и лимитов.

## Быстрый старт

```yaml
services:
  geo:
    image: aliakseikarpenka/slup-geo:latest
    restart: unless-stopped
    environment:
      PBF_URL: https://download.geofabrik.de/europe/belarus-latest.osm.pbf
    volumes:
      - geo_data:/data
volumes:
  geo_data:
```

Первый запуск скачивает PBF, импортирует его и собирает тайлы — минуты. Готовность:
`curl http://localhost:8080/health` → `{"status":"ok"}`. Демо: `http://localhost:8080/example`.

## Что внутри

- **API**: `GET /v1/suggest?q` (подсказки «улица → дома»), `GET /v1/point?lat&lon` (дом + зоны),
  `GET /v1/areas/{id}` (GeoJSON).
- **Карта**: `/tiles/style.json` + PMTiles + шрифты — MapLibre/pmtiles, всё своё, без CDN.
- **Движок**: обновление из PBF (вручную, при старте или по cron) без простоя API.

Полная документация, конфигурация и лицензии — в репозитории:
https://github.com/laantern/slup-geo

## Лицензия

MIT. Атрибуция карты: `© OpenMapTiles · © OpenStreetMap contributors`.
```

## Чек-лист видимости

- [x] Репозиторий: description и topics (см. выше), лицензия MIT, README с FAQ и сравнением.
- [x] Docker Hub: description и full description, Source repository → GitHub.
- [ ] Social preview и Releases с человекочитаемыми заметками (см. CHANGELOG.md).
- [ ] Добавить в каталоги: awesome-selfhosted, awesome-openstreetmap, awesome-geospatial,
      awesome-docker, selfh.st, alternativeto.net.
- [ ] Пост/статья: r/selfhosted, r/openstreetmap, Hacker News (Show HN), Habr/dev.to —
      «Как мы вынесли OSM-геокодер в один контейнер: PostGIS + tilemaker + PMTiles».
- [ ] Кросс-ссылки GitHub ↔ Docker Hub ↔ статья.
- [x] `llms.txt` и `AGENTS.md` для ИИ-агентов.
- [x] Multi-arch образ (`linux/amd64`, `linux/arm64`), SBOM и provenance.

## Релизы

Пуш в `master` автоматически: `test` + `integration` → тег `vX.Y.Z` (patch-бамп) → публикация
образа `aliakseikarpenka/slup-geo:<версия>` и `:latest`. Заметки релиза — из CHANGELOG.md.
