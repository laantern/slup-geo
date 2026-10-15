# Профиль tilemaker (схема OpenMapTiles)

Файлы из проекта [tilemaker](https://github.com/systemed/tilemaker) v3.2.0 (`resources/`):

- `config-openmaptiles.json` — слои и зумы (схема OpenMapTiles);
- `process-openmaptiles.lua` — преобразование тегов OSM в слои;
- `LICENCE.txt` — лицензия tilemaker (FTWPL). Уведомления библиотек, вкомпилированных в бинарник
  tilemaker (MIT/BSD/ISC/Apache-2.0/BSL-1.0), — в [`licenses/tilemaker-third-party.txt`](../../licenses/tilemaker-third-party.txt)
  (в образе — `/usr/share/doc/tilemaker/THIRD_PARTY_NOTICES.txt`).

**Изменения относительно апстрима:**
- из конфига удалены слои, требующие внешних shapefile-датасетов (`ocean`, `urban_areas`,
  `ice_shelf`, `glacier` — coastline/landcover, ~2 ГБ). Для сухопутных регионов они не нужны;
  остальные слои и настройки не менялись. Зумы: 0–14;
- в `process-openmaptiles.lua` выставлен `preferred_language = "ru"` — подписи карты на русском
  (`name:ru` → `name:latin`), с откатом на OSM `name`, если русского нет. Чтобы вернуть
  белорусские/исходные подписи — поставьте `nil` и пересоберите тайлы.
