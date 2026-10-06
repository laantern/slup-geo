# Веб-ассеты карты (стиль, шрифты, пример)

Всё, что нужно фронтенду для отрисовки, отдаётся сервисом из этого каталога (в образе —
`/usr/local/share/slup-geo/`).

| Файл | Что | Лицензия |
|---|---|---|
| `style.json` | стиль под схему OpenMapTiles; все ссылки относительные (`/tiles/...`), поэтому работает за любым доменом и прокси без настроек; без POI-иконок (спрайт не входит) | BSD-3-Clause (код) + CC-BY 4.0 (дизайн), см. `LICENSE-osm-bright.md` |
| `fonts/<fontstack>/<range>.pbf` | глифы Noto (Regular/Bold/Italic) для подписей | SIL OFL 1.1 (см. `fonts/LICENSE` и `fonts/NOTICE`) |
| `example/` | страница-пример `/example`: MapLibre + pmtiles (вендорные, без CDN), карта, поиск, точка, границы | MapLibre GL JS — BSD-3-Clause; pmtiles JS — BSD-3-Clause |

**Обязательная атрибуция.** Продукты, использующие карту на схеме OpenMapTiles, должны заметно
указывать «OpenMapTiles» со ссылкой на https://openmaptiles.org/ — например, в углу карты:
`© OpenMapTiles · © OpenStreetMap contributors`. Дизайн стиля (CC-BY 4.0) требует доступной
атрибуции на странице с картой; в углу карты достаточно строки выше.

**Источники:**
- стиль — [openmaptiles/osm-bright-gl-style](https://github.com/openmaptiles/osm-bright-gl-style)
  (BSD-3; заменены `sources` (на `pmtiles:///tiles/basemap.pmtiles`) и `glyphs` на наш сервис,
  удалены спрайт и иконки: MapLibre требует абсолютный URL спрайта, а стиль должен оставаться
  статическим; при необходимости добавьте свой спрайт с абсолютным URL);
- глифы — [OpenFreeMap](https://openfreemap.org) (сгенерированы из Noto, OFL 1.1); в репозиторий
  положены нужные диапазоны (латиница, кириллица, пунктуация, символы) — 11 диапазонов на каждый
  из трёх начертаний.

Если нужны подписи в других письменностях (CJK и т.п.) — добавьте соответствующие диапазоны
глифов в `fonts/<fontstack>/` (имя файла — `<начало>-<конец>.pbf`).
