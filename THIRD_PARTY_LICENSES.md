# Сторонние компоненты и лицензии

`slup-geo` распространяется под MIT (см. [LICENSE](LICENSE)). В образе и репозитории используются
сторонние компоненты — ниже их лицензии и источники.

## Образ (runtime)

| Компонент | Версия | Лицензия | Источник |
|---|---|---|---|
| PostgreSQL | 16 (Debian package) | PostgreSQL License (BSD-подобная) | https://www.postgresql.org |
| PostGIS | 3.6 (Debian package) | GPL-2+ (части — Apache-2.0/BSD-3) | https://postgis.net |
| osm2pgsql | 1.8 (Debian package) | GPL-2+ | https://osm2pgsql.org |
| tilemaker | 3.2.0 (сборка из исходников) | FTWPL; вложенные библиотеки — Boost/MIT/BSD/Apache | https://github.com/systemed/tilemaker |
| Boost | 1.74 (Debian runtime) | BSL-1.0 | https://www.boost.org |
| Lua | 5.3 (Debian runtime) | MIT | https://www.lua.org |
| SQLite | 3.x (Debian runtime) | Public domain | https://sqlite.org |
| shapelib | 1.5 (Debian runtime) | MIT / LGPL-2+ | http://shapelib.maptools.org |
| Debian base image | bookworm | Разные (см. `/usr/share/doc/*/copyright` в образе) | https://www.debian.org |

PostGIS, osm2pgsql и tilemaker — отдельные программы (GPL/FTWPL); они собраны в один образ как
агрегат и не меняют лицензию нашего кода. Тексты лицензий включены в образ
(`/usr/share/doc/<пакет>/copyright`), исходники — по ссылкам выше (для GPL-компонентов это
стандартное требование при распространении образа).

## Go-зависимости

| Модуль | Лицензия |
|---|---|
| github.com/jackc/pgx/v5 (+ pgpassfile, pgservicefile, puddle) | MIT |
| github.com/robfig/cron/v3 | MIT |
| golang.org/x/text, golang.org/x/sync | BSD-3-Clause |

## Данные и картография

| Артефакт | Лицензия | Примечание |
|---|---|---|
| Данные OpenStreetMap (PBF) | ODbL 1.0 | Обязательна атрибуция «© OpenStreetMap contributors»; данные скачиваются пользователем, в образе их нет |
| Профиль tilemaker (`tiles/config-openmaptiles.json`, `tiles/process-openmaptiles.lua`) | FTWPL (tilemaker) | Взято из tilemaker v3.2.0; из конфига удалены слои, требующие внешних shapefile-датасетов |
| Схема OpenMapTiles | BSD-3-Clause | Имена слоёв/полей в тайлах |

## Стиль и фронтенд (следующий этап)

| Компонент | Лицензия |
|---|---|
| MapLibre GL JS | BSD-3-Clause |
| pmtiles JS | BSD-3-Clause |
| OSM Bright (основа стиля) | BSD-3-Clause (MapTiler/Mapbox) |
| Шрифты Noto (глифы) | SIL OFL 1.1 |
