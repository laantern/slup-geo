-- Поисковый индекс имён: по строке на вариант имени (name / name:ru / name:be) для зон и улиц.
-- Для улиц дополнительно хранится вариант без типового слова («улица», «вуліца», «переулок», «завулак»…):
-- тогда запрос «Бородина» точно матчит «улица Бородина», как люди и ищут.
-- Дома не индексируем: они находятся через улицу (иначе миллионы строк и тяжёлый GIN).
-- name_norm: lower, ё→е, пунктуация → пробелы (единая нормализация с запросом).
-- Варианты с одинаковой нормой схлопываются, чтобы каждая сущность+норма была уникальной
-- (уникальный индекс нужен для REFRESH MATERIALIZED VIEW CONCURRENTLY).

CREATE MATERIALIZED VIEW geo.names AS
WITH base AS (
  SELECT 'AREA'::text AS entity_kind,
         z.osm_type || z.osm_id AS entity_id,
         v.name::text AS name,
         regexp_replace(
           regexp_replace(replace(lower(v.name::text), 'ё', 'е'), '[^а-яa-z0-9]+', ' ', 'g'),
           '([0-9])([а-яa-z])', '\1 \2', 'g'
         ) AS name_norm
  FROM geo.zones z
  CROSS JOIN LATERAL (VALUES (z.label), (z.name_ru), (z.name_be)) AS v(name)
  WHERE v.name IS NOT NULL AND btrim(v.name::text) <> ''
  UNION ALL
  SELECT 'STREET'::text,
         s.osm_type || s.osm_id,
         v.name::text,
         regexp_replace(
           regexp_replace(replace(lower(v.name::text), 'ё', 'е'), '[^а-яa-z0-9]+', ' ', 'g'),
           '([0-9])([а-яa-z])', '\1 \2', 'g'
         )
  FROM geo.streets s
  CROSS JOIN LATERAL (VALUES (s.label), (s.name_ru), (s.name_be)) AS v(name)
  WHERE v.name IS NOT NULL AND btrim(v.name::text) <> ''
),
stripped AS (
  SELECT entity_kind, entity_id, name,
         btrim(regexp_replace(
           name_norm,
           '^(улица|вуліца|ул|переулок|пер|завулак|проспект|праспект|проезд|праезд|площадь|плошча|шоссе|шаша|тупик|тупік|аллея|алея|бульвар|набережная|набярэжная|тракт|дорога|дарога) | (улица|вуліца|ул|переулок|пер|завулак|проспект|праспект|проезд|праезд|площадь|плошча|шоссе|шаша|тупик|тупік|аллея|алея|бульвар|набережная|набярэжная|тракт|дорога|дарога)$',
           '', 'g'
         )) AS name_norm
  FROM base
  WHERE entity_kind = 'STREET'
),
dedup AS (
  SELECT entity_kind, entity_id, name_norm, min(name) AS name
  FROM (
    SELECT entity_kind, entity_id, name, name_norm FROM base
    UNION ALL
    SELECT entity_kind, entity_id, name, name_norm FROM stripped
  ) t
  WHERE name_norm <> ''
  GROUP BY entity_kind, entity_id, name_norm
)
SELECT entity_kind, entity_id, name, name_norm
FROM dedup
WHERE btrim(name_norm) <> '';

CREATE INDEX names_norm_trgm_idx ON geo.names USING gin (name_norm gin_trgm_ops);
CREATE INDEX names_norm_prefix_idx ON geo.names (name_norm text_pattern_ops);
CREATE UNIQUE INDEX names_entity_uidx ON geo.names (entity_kind, entity_id, name_norm);
