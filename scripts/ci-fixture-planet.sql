-- Минимальные planet_osm_* таблицы с эталонными объектами для CI-проверки SQL-схемы geo.
-- Повторяет структуру, которую создаёт osm2pgsql --hstore-all --latlong (только нужные колонки).
DROP TABLE IF EXISTS planet_osm_point, planet_osm_line, planet_osm_polygon, planet_osm_roads CASCADE;

CREATE TABLE planet_osm_roads (
    osm_id  bigint,
    name    text,
    highway text,
    tags    hstore,
    way     geometry(Geometry, 4326)
);

CREATE TABLE planet_osm_polygon (
    osm_id   bigint,
    name     text,
    boundary text,
    landuse  text,
    tags     hstore,
    way      geometry(Geometry, 4326)
);

CREATE TABLE planet_osm_line (
    osm_id  bigint,
    name    text,
    highway text,
    tags    hstore,
    way     geometry(Geometry, 4326)
);

CREATE TABLE planet_osm_point (
    osm_id bigint,
    tags   hstore,
    way    geometry(Geometry, 4326)
);

-- Область с русским именем (проверяет name:ru в matview zones).
INSERT INTO planet_osm_polygon (osm_id, name, boundary, landuse, tags, way) VALUES
(1, 'Гомельская область', 'administrative', NULL,
 '"name:ru"=>"Гомельская область", "admin_level"=>"4"'::hstore,
 ST_GeomFromText('POLYGON((30.0 52.0, 31.5 52.0, 31.5 53.0, 30.0 53.0, 30.0 52.0))', 4326)),
-- Дом-полигон с адресом.
(2, NULL, NULL, NULL,
 '"addr:housenumber"=>"2", "addr:street"=>"улица Бородина"'::hstore,
 ST_GeomFromText('POLYGON((30.9500 52.3900, 30.9510 52.3900, 30.9510 52.3910, 30.9500 52.3910, 30.9500 52.3900))', 4326));

-- Улица Бородина.
INSERT INTO planet_osm_line (osm_id, name, highway, tags, way) VALUES
(10, 'улица Бородина', 'residential', '"name:ru"=>"улица Бородина"'::hstore,
 ST_GeomFromText('LINESTRING(30.9400 52.3900, 30.9600 52.4000)', 4326));

-- Адрес-нода (проверяет Point-геометрию в /v1/areas).
INSERT INTO planet_osm_point (osm_id, tags, way) VALUES
(20, '"addr:housenumber"=>"4", "addr:street"=>"улица Бородина"'::hstore,
 ST_GeomFromText('POINT(30.9550 52.3950)', 4326));
