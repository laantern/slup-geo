-- Слой представлений поверх данных osm2pgsql.
-- Применяется после импорта: планета-таблицы пересоздаются, поэтому матвью пересобираются.
-- Уникальные индексы нужны для REFRESH MATERIALIZED VIEW CONCURRENTLY (обновление без
-- блокировки чтения при будущем append-импорте) и заодно защищают от дублей.

CREATE SCHEMA IF NOT EXISTS geo;

DROP MATERIALIZED VIEW IF EXISTS geo.names;
DROP MATERIALIZED VIEW IF EXISTS geo.addresses;
DROP MATERIALIZED VIEW IF EXISTS geo.streets;
DROP MATERIALIZED VIEW IF EXISTS geo.zones;

CREATE MATERIALIZED VIEW geo.zones AS
WITH parts AS (
  SELECT
    'W'::text AS osm_type,
    osm_id,
    COALESCE(tags->'name:ru', name) AS label,
    name AS name_be,
    tags->'name:ru' AS name_ru,
    CASE
      WHEN tags->'place' IN ('city', 'town') THEN 'CITY'
      WHEN tags->'place' IN ('village', 'hamlet') THEN 'SETTLEMENT'
      WHEN tags->'place' IN ('suburb', 'borough') THEN 'CITY_DISTRICT'
      WHEN tags->'place' = 'neighbourhood' THEN 'NEIGHBOURHOOD'
      WHEN tags->'place' = 'quarter' THEN 'QUARTER'
      WHEN tags->'admin_level' = '2' THEN 'COUNTRY'
      WHEN tags->'admin_level' = '4' THEN 'OBLAST'
      WHEN tags->'admin_level' = '6' THEN 'RAION'
      WHEN tags->'admin_level' = '9' THEN 'CITY_DISTRICT'
      WHEN tags->'admin_level' = '8' THEN 'SETTLEMENT'
      WHEN tags->'landuse' = 'residential' THEN 'RESIDENTIAL'
      WHEN tags->'landuse' IS NOT NULL THEN 'LANDUSE'
      ELSE 'AREA'
    END AS level,
    CASE
      WHEN boundary = 'administrative' THEN 'ADMIN'
      WHEN tags ? 'place' THEN 'PLACE'
      WHEN tags ? 'landuse' THEN 'LANDUSE'
      ELSE 'AREA'
    END AS kind,
    way AS geom
  FROM planet_osm_polygon
  WHERE (boundary = 'administrative'
     OR tags ? 'place'
     OR landuse IN ('residential', 'industrial', 'commercial', 'retail', 'construction', 'cemetery', 'allotments'))
    AND COALESCE(tags->'name:ru', name) IS NOT NULL
),
merged AS (
  -- osm2pgsql хранит мультиполигоны отношения несколькими строками (по части на строку):
  -- склеиваем части в один объект, иначе объект «теряет» эксклавы
  SELECT
    osm_type,
    osm_id,
    min(label) AS label,
    min(name_be) AS name_be,
    min(name_ru) AS name_ru,
    min(level) AS level,
    min(kind) AS kind,
    ST_Union(geom) AS geom,
    sum(ST_Area(geom::geography)) AS area_m2
  FROM parts
  GROUP BY osm_type, osm_id
)
SELECT
  osm_type,
  osm_id,
  label,
  name_be,
  name_ru,
  level,
  kind,
  area_m2,
  geom,
  -- упрощённая геометрия для отдачи на фронт (границы в UI): допуск зависит от уровня,
  -- считается один раз при сборке, в запросе просто читается колонка
  CASE
    WHEN level IN ('OBLAST', 'COUNTRY') THEN ST_SimplifyPreserveTopology(geom, 0.01)
    WHEN level = 'RAION' THEN ST_SimplifyPreserveTopology(geom, 0.002)
    ELSE ST_SimplifyPreserveTopology(geom, 0.0005)
  END AS geom_display
FROM merged;

CREATE INDEX zones_geom_idx ON geo.zones USING gist (geom);
CREATE INDEX zones_level_idx ON geo.zones (level);
CREATE UNIQUE INDEX zones_osm_uidx ON geo.zones (osm_type, osm_id);

CREATE MATERIALIZED VIEW geo.streets AS
SELECT
  'W'::text AS osm_type,
  osm_id,
  min(name) AS name_be,
  min(tags->'name:ru') AS name_ru,
  min(COALESCE(tags->'name:ru', name)) AS label,
  ST_Union(way) AS geom
FROM planet_osm_line
WHERE highway IS NOT NULL AND name IS NOT NULL
GROUP BY osm_id;

CREATE INDEX streets_geom_idx ON geo.streets USING gist (geom);
CREATE INDEX streets_name_be_idx ON geo.streets (name_be);
CREATE INDEX streets_osm_idx ON geo.streets (osm_id);
CREATE UNIQUE INDEX streets_osm_uidx ON geo.streets (osm_type, osm_id);

CREATE MATERIALIZED VIEW geo.addresses AS
SELECT 'W'::text AS osm_type, osm_id,
       min(tags->'addr:housenumber') AS housenumber,
       min(tags->'addr:street') AS street_be,
       min(tags->'addr:city') AS city,
       ST_PointOnSurface(ST_Union(way)) AS point,
       ST_Union(way) AS geom
FROM planet_osm_polygon
WHERE tags ? 'addr:housenumber'
GROUP BY osm_id
UNION ALL
SELECT 'N'::text, osm_id,
       tags->'addr:housenumber',
       tags->'addr:street',
       tags->'addr:city',
       way,
       way
FROM planet_osm_point
WHERE tags ? 'addr:housenumber';

-- geom — контур дома: расстояние до него = 0 для точки внутри/на доме (нужно для порога «точный адрес»)
CREATE INDEX addresses_geom_idx ON geo.addresses USING gist (geom);
CREATE INDEX addresses_street_idx ON geo.addresses (street_be);
CREATE UNIQUE INDEX addresses_osm_uidx ON geo.addresses (osm_type, osm_id);
