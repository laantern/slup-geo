-- Отладка локации точки внутри контейнера (psql):
--   docker compose exec -T geo psql -U geo_user -d geo_db -v lon=30.96 -v lat=52.39 < scripts/debug-point.sql
-- Показывает дом (если в пределах 5 м от контура) и содержащие зоны по возрастанию площади —
-- те же данные, что собирает GET /v1/point.
\timing on
WITH p AS (
  SELECT ST_SetSRID(ST_MakePoint(:'lon'::float8, :'lat'::float8), 4326) AS g
),
house AS (
  SELECT a.osm_type || a.osm_id AS id,
         concat_ws(', ', a.housenumber, COALESCE(s.label, a.street_be)) AS name,
         'HOUSE' AS level, 'BUILDING' AS kind,
         round(ST_Area(a.geom::geography)) AS area_m2,
         0 AS grp
  FROM geo.addresses a, p
  LEFT JOIN LATERAL (SELECT label FROM geo.streets st WHERE st.name_be = a.street_be LIMIT 1) s ON true
  WHERE ST_DWithin(a.geom, p.g, 0.0001)
    AND ST_Distance(a.geom::geography, p.g::geography) <= 5
  ORDER BY a.geom <-> p.g
  LIMIT 1
),
areas AS (
  SELECT z.osm_type || z.osm_id AS id, z.label AS name, z.level, z.kind, z.area_m2, 1 AS grp
  FROM geo.zones z, p
  WHERE ST_Covers(z.geom, p.g)
),
zones AS (
  SELECT * FROM house
  UNION ALL
  SELECT * FROM areas
)
SELECT jsonb_pretty(jsonb_build_object(
  'request', jsonb_build_object('lat', :'lat'::float8, 'lon', :'lon'::float8),
  'zones', (SELECT jsonb_agg(jsonb_build_object(
              'name', z.name, 'level', z.level, 'kind', z.kind, 'areaM2', z.area_m2)
              ORDER BY z.grp, z.area_m2)
            FROM zones z)
)) AS response;
