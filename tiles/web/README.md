# Веб-ассеты карты (стиль, шрифты, пример)

Всё, что нужно фронтенду для отрисовки, отдаётся сервисом из этого каталога (в образе —
`/usr/local/share/slup-geo/`).

| Файл | Что | Лицензия |
|---|---|---|
| `style.json` | стиль под схему OpenMapTiles; ссылки абсолютные (`/tiles/...`) — работает за любым доменом при размещении в корне (для подпути поправьте пути); без POI-иконок (спрайт не входит) | BSD-3-Clause (код) + CC-BY 4.0 (дизайн), см. `LICENSE-osm-bright.md` |
| `fonts/<fontstack>/<range>.pbf` | глифы Noto (Regular/Bold/Italic) для подписей | SIL OFL 1.1 (см. `fonts/LICENSE` и `fonts/NOTICE`) |
| `vendor/` | вендорные библиотеки карты (без CDN): MapLibre GL JS 6.12.0 — ESM-модули `maplibre-gl.mjs` + `maplibre-gl-shared.mjs` + `maplibre-gl-worker.mjs`, `maplibre-gl.css`; pmtiles JS 4.5.0 (`pmtiles.js`) | MapLibre — BSD-3-Clause; pmtiles JS — BSD-3-Clause |
| `example/` | страница-пример `/example` (`index.html` + `app.js`): карта, поиск, точка, границы; библиотеки подключаются из `../tiles/vendor/` | см. `vendor/` |

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
  из трёх начертаний;
- библиотеки — [MapLibre GL JS](https://maplibre.org/) 6.12.0 (ESM, без сборщика) и
  [pmtiles JS](https://github.com/protomaps/PMTiles) 4.5.0; страница-пример подключает их
  относительными путями `../tiles/vendor/...`.

Если нужны подписи в других письменностях (CJK и т.п.) — добавьте соответствующие диапазоны
глифов в `fonts/<fontstack>/` (имя файла — `<начало>-<конец>.pbf`).
