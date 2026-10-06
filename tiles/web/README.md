# Веб-ассеты карты (стиль, спрайт, шрифты)

Всё, что нужно фронтенду для отрисовки, отдаётся сервисом из этого каталога (в образе —
`/usr/local/share/slup-geo/`).

| Файл | Что | Лицензия |
|---|---|---|
| `style.json` | стиль под схему OpenMapTiles; все ссылки относительные (`/tiles/...`), поэтому работает за любым доменом и прокси без настроек | BSD-3-Clause (OSM Bright, см. `LICENSE-osm-bright.md`) |
| `sprite.json`, `sprite.png`, `sprite@2x.*` | иконки стиля | BSD-3-Clause (тот же стиль) |
| `fonts/<fontstack>/<range>.pbf` | глифы Noto (Regular/Bold/Italic) для подписей | SIL OFL 1.1 (см. `fonts/LICENSE`) |

**Источники:**
- стиль и спрайт — [openmaptiles/osm-bright-gl-style](https://github.com/openmaptiles/osm-bright-gl-style)
  (BSD-3; в стиле заменены `sources` (на `pmtiles:///tiles/basemap.pmtiles`), `sprite` и `glyphs` на наш сервис);
- глифы — [OpenFreeMap](https://openfreemap.org) (сгенерированы из Noto, OFL 1.1); в репозиторий
  положены нужные диапазоны (латиница, кириллица, пунктуация, символы) — 11 диапазонов на каждый
  из трёх начертаний.

Если нужны подписи в других письменностях (CJK и т.п.) — добавьте соответствующие диапазоны
глифов в `fonts/<fontstack>/` (имя файла — `<начало>-<конец>.pbf`).
