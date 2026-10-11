# Сторонние компоненты и лицензии

Код `slup-geo` распространяется под MIT (см. [LICENSE](LICENSE)). Docker-образ — агрегат: кроме
нашего кода в нём есть сторонние программы и библиотеки под своими лицензиями (в том числе
GPL-2+). Ниже — их лицензии, источники и то, где в образе лежат тексты лицензий.

## Где лицензии в образе

| Путь в образе | Что |
|---|---|
| `/usr/share/doc/slup-geo/LICENSE` | лицензия кода slup-geo (MIT) |
| `/usr/share/doc/slup-geo/THIRD_PARTY_LICENSES.md` | этот документ |
| `/usr/share/doc/slup-geo/go-licenses/` | тексты лицензий стандартной библиотеки Go и каждого Go-модуля, вкомпилированного в `slup-geo` (собираются при сборке образа; модуль без лицензии валит сборку) |
| `/usr/share/doc/slup-geo/debian-packages.tsv` | точные версии всех пакетов Debian/PGDG в образе и их исходных пакетов |
| `/usr/share/doc/<пакет>/copyright` | лицензии пакетов Debian/PGDG (PostgreSQL, PostGIS, osm2pgsql, Boost, Lua, SQLite, shapelib и базовой системы) |
| `/usr/share/doc/tilemaker/LICENCE.txt`, `THIRD_PARTY_NOTICES.txt` | лицензия tilemaker и уведомления вкомпилированных в него библиотек (исходник — [licenses/tilemaker-third-party.txt](licenses/tilemaker-third-party.txt)) |
| `/usr/local/share/slup-geo/LICENSE-osm-bright.md` | стиль OSM Bright (код BSD-3, дизайн CC-BY 4.0) |
| `/usr/local/share/slup-geo/fonts/LICENSE`, `NOTICE` | глифы Noto (SIL OFL 1.1) |
| `/usr/local/share/slup-geo/vendor/LICENSE-*.txt` | MapLibre GL JS и pmtiles JS (BSD-3-Clause) |

## Образ (runtime)

| Компонент | Версия | Лицензия | Источник |
|---|---|---|---|
| PostgreSQL | 16 (пакет PGDG из базового образа `postgres:16-bookworm`) | PostgreSQL License (BSD-подобная) | https://www.postgresql.org |
| PostGIS | 3.6 (пакет PGDG `postgresql-16-postgis-3`) | GPL-2+ (части — Apache-2.0/BSD/MIT) | https://postgis.net |
| osm2pgsql | 1.8 (пакет Debian bookworm) | GPL-2+ | https://osm2pgsql.org |
| tilemaker | 3.2.0 (сборка из исходников, sha256 архива закреплён в Dockerfile) | FTWPL | https://github.com/systemed/tilemaker |
| Boost | 1.74 (Debian runtime + header-only при сборке tilemaker) | BSL-1.0 | https://www.boost.org |
| Lua | 5.3 (Debian runtime) | MIT | https://www.lua.org |
| SQLite | 3.x (Debian runtime) | Public domain | https://sqlite.org |
| shapelib | 1.5 (Debian runtime) | MIT / LGPL-2+ | http://shapelib.maptools.org |
| Debian base image | bookworm | Разные (см. `/usr/share/doc/*/copyright`) | https://www.debian.org |

Точные версии пакетов конкретной сборки — в `/usr/share/doc/slup-geo/debian-packages.tsv`
(apt-пакеты не пиннятся, чтобы образ получал security-обновления при пересборке).

### Исходники GPL-компонентов

PostGIS, osm2pgsql и другие GPL-пакеты образа — отдельные программы, собранные с нашим кодом
в один образ как агрегат; лицензию кода slup-geo они не меняют. GPL требует, чтобы получатель
бинарников мог получить соответствующие исходники. Для версии из `debian-packages.tsv`
(колонки «исходный пакет» и «версия исходного пакета»):

- пакеты Debian — `apt-get source <исходный пакет>=<версия>` или
  `https://snapshot.debian.org/package/<исходный пакет>/<версия>/`;
- пакеты PGDG (PostgreSQL, PostGIS) — `https://apt.postgresql.org/pub/repos/apt/pool/main/`
  (актуальные) и `https://apt-archive.postgresql.org/` (архив прежних версий);
- tilemaker — архив `https://github.com/systemed/tilemaker/archive/refs/tags/v3.2.0.tar.gz`
  (sha256 закреплён в Dockerfile).

### Библиотеки, вкомпилированные в tilemaker

tilemaker включает в свои исходники сторонние библиотеки; их уведомления обязательны при
бинарной поставке и лежат в `/usr/share/doc/tilemaker/THIRD_PARTY_NOTICES.txt`.

| Библиотека | Лицензия |
|---|---|
| libdeflate | MIT |
| libpopcnt | BSD-2-Clause |
| protozero, vtzero | BSD-2-Clause |
| PMTiles (C++) | BSD-3-Clause |
| polylabel | ISC |
| sqlite_modern_cpp | MIT |
| visvalingam.cpp (на основе orb и tippecanoe) | MIT / BSD-2-Clause |
| streamvbyte (+ фрагмент PyTorch TH/simd) | Apache-2.0 (фрагмент — BSD-3-Clause) |
| kaguya | BSL-1.0 |
| RapidJSON (header-only, Debian `rapidjson-dev`) | MIT |

FTWPL (лицензия самого tilemaker) не одобрена OSI и не содержит явного патентного гранта —
учитывайте это, если у вашей организации есть политика допустимых лицензий.

## Go-зависимости

Вкомпилированы в бинарник `slup-geo`; тексты лицензий — в `/usr/share/doc/slup-geo/go-licenses/`.

| Модуль | Лицензия |
|---|---|
| Стандартная библиотека Go | BSD-3-Clause |
| github.com/jackc/pgx/v5 (+ pgpassfile, pgservicefile, puddle) | MIT |
| github.com/robfig/cron/v3 | MIT |
| golang.org/x/text, golang.org/x/sync | BSD-3-Clause |

## Данные и картография

| Артефакт | Лицензия | Примечание |
|---|---|---|
| Данные OpenStreetMap (PBF) | ODbL 1.0 | В образе их нет: PBF скачивается пользователем. Атрибуция «© OpenStreetMap contributors» обязательна |
| Производные данные: БД `geo.*`, ответы `/v1/*`, тайлы `*.pmtiles` | ODbL 1.0 | Публично раздаваемые тайлы — производная база данных OSM: распространяются на условиях ODbL с атрибуцией |
| Профиль tilemaker (`tiles/tilemaker/config-openmaptiles.json`, `tiles/tilemaker/process-openmaptiles.lua`) | FTWPL (tilemaker) | Взято из tilemaker v3.2.0; изменения — в `tiles/tilemaker/README.md` |
| Схема OpenMapTiles | код BSD-3-Clause, схема и дизайн CC-BY 4.0 | Имена слоёв/полей в тайлах; требует заметной атрибуции «© OpenMapTiles» со ссылкой на https://openmaptiles.org/ |

## Стиль и фронтенд

| Компонент | Лицензия | Текст в образе |
|---|---|---|
| MapLibre GL JS 6.12.0 (вендорно: `tiles/web/vendor/maplibre-gl.mjs`, `maplibre-gl-shared.mjs`, `maplibre-gl-worker.mjs`, `maplibre-gl.css`) | BSD-3-Clause (включает код Mapbox под BSD-3 и сторонние части — см. файл) | `vendor/LICENSE-maplibre-gl.txt` |
| pmtiles JS 4.5.0 (вендорно: `tiles/web/vendor/pmtiles.js`) | BSD-3-Clause | `vendor/LICENSE-pmtiles.txt` |
| OSM Bright: `tiles/web/style.json` (код) | BSD-3-Clause (MapTiler/Mapbox) | `LICENSE-osm-bright.md` |
| OSM Bright: дизайн стиля | CC-BY 4.0 (атрибуция на странице с картой) | `LICENSE-osm-bright.md` |
| Глифы Noto (`tiles/web/fonts`) | SIL OFL 1.1, Reserved Font Names не заявлены | `fonts/LICENSE`, `fonts/NOTICE` |

MapLibre GL JS 6.x распространяется в виде ES-модулей — страница-пример подключает их
напрямую из `/tiles/vendor/*` (без CDN и сборщика).

**Требование атрибуции:** карта на схеме OpenMapTiles должна заметно указывать
«© OpenMapTiles» (ссылка на https://openmaptiles.org/) и «© OpenStreetMap contributors».
Эта строка есть в `/example`, в `source.attribution` стиля, в метаданных PMTiles и в `tiles.json`.
