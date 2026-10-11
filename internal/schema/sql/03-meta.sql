-- Версия схемы данных (geo.meta.schema_version). {{version}} подставляет internal/schema
-- из schema.Version — единственный источник номера версии.
INSERT INTO geo.meta (key, value) VALUES ('schema_version', '{{version}}')
ON CONFLICT (key) DO UPDATE SET value = excluded.value;
