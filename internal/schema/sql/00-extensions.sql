-- Расширения: PostGIS — пространственные типы/функции, hstore — osm2pgsql --hstore-all,
-- pg_trgm — поиск имён. Идемпотентно, применяется перед импортом.
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS hstore;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
