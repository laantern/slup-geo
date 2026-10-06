package geo

import (
	"context"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore — реализация всех портов хранилища поверх PostgreSQL/PostGIS.
type PGStore struct {
	pool *pgxpool.Pool
}

// NewPGStore создаёт хранилище.
func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

// NearestHouse ищет дом в пределах 5 м от контура (точка внутри контура — расстояние 0).
func (s *PGStore) NearestHouse(ctx context.Context, lat, lon float64) (*House, error) {
	row := s.pool.QueryRow(ctx, nearestHouseSQL, pgx.NamedArgs{"lat": lat, "lon": lon})

	var (
		id          string
		housenumber *string
		street      *string
		areaM2      float64
	)
	if err := row.Scan(&id, &housenumber, &street, &areaM2); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &House{ID: id, Housenumber: housenumber, Street: street, AreaM2: int64(math.Round(areaM2))}, nil
}

// ZonesAt возвращает покрывающие точку зоны по возрастанию площади.
func (s *PGStore) ZonesAt(ctx context.Context, lat, lon float64) ([]ZoneRow, error) {
	rows, err := s.pool.Query(ctx, zonesAtSQL, pgx.NamedArgs{"lat": lat, "lon": lon})
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var zones []ZoneRow
	for rows.Next() {
		var (
			id     string
			name   *string
			level  string
			kind   string
			areaM2 float64
		)
		if err := rows.Scan(&id, &name, &level, &kind, &areaM2); err != nil {
			return nil, err
		}
		zoneName := id
		if name != nil {
			zoneName = *name
		}
		zones = append(zones, ZoneRow{
			ID:     id,
			Name:   zoneName,
			Level:  ZoneLevelFromOSM(level),
			Kind:   ZoneKindFromOSM(kind),
			AreaM2: int64(math.Round(areaM2)),
		})
	}
	return zones, rows.Err()
}

// Streets ищет улицы по нормализованному имени.
func (s *PGStore) Streets(ctx context.Context, nameNorm string, limit int) ([]StreetMatch, error) {
	rows, err := s.pool.Query(ctx, findStreetsSQL, pgx.NamedArgs{"nameNorm": nameNorm, "limit": limit})
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []StreetMatch
	for rows.Next() {
		var (
			osmID      int64
			label      string
			bestNorm   string
			matchRank  int
			similarity float64
			cityLabel  *string
			lat, lon   float64
		)
		if err := rows.Scan(&osmID, &label, &bestNorm, &matchRank, &similarity, &cityLabel, &lat, &lon); err != nil {
			return nil, err
		}
		matches = append(matches, StreetMatch{
			OSMID:      osmID,
			Label:      label,
			NameNorm:   bestNorm,
			MatchRank:  matchRank,
			Similarity: similarity,
			CityLabel:  cityLabel,
			Lat:        lat,
			Lon:        lon,
		})
	}
	return matches, rows.Err()
}

// Areas ищет зоны по нормализованному имени.
func (s *PGStore) Areas(ctx context.Context, nameNorm string, limit int) ([]AreaMatch, error) {
	rows, err := s.pool.Query(ctx, findAreasSQL, pgx.NamedArgs{"nameNorm": nameNorm, "limit": limit})
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []AreaMatch
	for rows.Next() {
		var (
			id          string
			label       string
			level       string
			kind        string
			areaM2      float64
			matchRank   int
			similarity  float64
			parentLabel *string
			lat, lon    float64
		)
		if err := rows.Scan(&id, &label, &level, &kind, &areaM2, &matchRank, &similarity, &parentLabel, &lat, &lon); err != nil {
			return nil, err
		}
		matches = append(matches, AreaMatch{
			ID:         id,
			Name:       label,
			Level:      ZoneLevelFromOSM(level),
			Kind:       ZoneKindFromOSM(kind),
			AreaM2:     int64(math.Round(areaM2)),
			Subtitle:   parentLabel,
			MatchRank:  matchRank,
			Similarity: similarity,
			Lat:        lat,
			Lon:        lon,
		})
	}
	return matches, rows.Err()
}

// StreetHouses возвращает дома улицы (с фильтром по номеру, если он задан).
func (s *PGStore) StreetHouses(ctx context.Context, streetOSMID int64, housenumber *string, limit int) ([]HouseMatch, error) {
	sql := housesFirstSQL
	args := pgx.NamedArgs{"osmID": streetOSMID, "limit": limit}
	if housenumber != nil {
		sql = housesByNumberSQL
		args["housenumber"] = *housenumber
	}

	rows, err := s.pool.Query(ctx, sql, args)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []HouseMatch
	for rows.Next() {
		var (
			id       string
			number   *string
			subtitle *string
			areaM2   float64
			lat, lon float64
		)
		if err := rows.Scan(&id, &number, &subtitle, &areaM2, &lat, &lon); err != nil {
			return nil, err
		}
		matches = append(matches, HouseMatch{
			ID:       id,
			Number:   number,
			Subtitle: subtitle,
			AreaM2:   int64(math.Round(areaM2)),
			Lat:      lat,
			Lon:      lon,
		})
	}
	return matches, rows.Err()
}

// StreetZones возвращает зоны, которые пересекает улица.
func (s *PGStore) StreetZones(ctx context.Context, streetOSMID int64, limit int) ([]ZoneMatch, error) {
	rows, err := s.pool.Query(ctx, findStreetZonesSQL, pgx.NamedArgs{"osmID": streetOSMID, "limit": limit})
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []ZoneMatch
	for rows.Next() {
		var (
			id       string
			name     string
			level    string
			kind     string
			areaM2   float64
			lat, lon float64
		)
		if err := rows.Scan(&id, &name, &level, &kind, &areaM2, &lat, &lon); err != nil {
			return nil, err
		}
		matches = append(matches, ZoneMatch{
			ID:     id,
			Name:   name,
			Level:  ZoneLevelFromOSM(level),
			Kind:   ZoneKindFromOSM(kind),
			AreaM2: int64(math.Round(areaM2)),
			Lat:    lat,
			Lon:    lon,
		})
	}
	return matches, rows.Err()
}

// FindArea ищет зону (geom_display для отрисовки) или дом (контур).
func (s *PGStore) FindArea(ctx context.Context, osmType string, osmID int64, exact bool) (*AreaRow, error) {
	row := s.pool.QueryRow(ctx, findAreaSQL, pgx.NamedArgs{"type": osmType, "osmID": osmID, "exact": exact})

	var (
		src     string
		id      string
		name    *string
		level   string
		kind    string
		areaM2  float64
		geojson *string
	)
	if err := row.Scan(&src, &id, &name, &level, &kind, &areaM2, &geojson); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	rowName := id
	if name != nil {
		rowName = *name
	}
	geometry := []byte("null")
	if geojson != nil {
		geometry = []byte(*geojson)
	}
	return &AreaRow{
		ID:       id,
		Name:     rowName,
		Level:    ZoneLevelFromOSM(level),
		Kind:     ZoneKindFromOSM(kind),
		AreaM2:   int64(math.Round(areaM2)),
		Geometry: geometry,
	}, nil
}

const nearestHouseSQL = `
SELECT a.osm_type || a.osm_id AS id,
       a.housenumber,
       COALESCE(s.label, a.street_be) AS street,
       round(ST_Area(a.geom::geography)) AS area_m2
FROM geo.addresses a
LEFT JOIN LATERAL (
    SELECT label FROM geo.streets st WHERE st.name_be = a.street_be LIMIT 1
) s ON true
WHERE ST_DWithin(a.geom, ST_SetSRID(ST_MakePoint(@lon, @lat), 4326), 0.0001)
  AND ST_Distance(a.geom::geography, ST_SetSRID(ST_MakePoint(@lon, @lat), 4326)::geography) <= 5
ORDER BY a.geom <-> ST_SetSRID(ST_MakePoint(@lon, @lat), 4326)
LIMIT 1`

const zonesAtSQL = `
SELECT z.osm_type || z.osm_id AS id,
       z.label AS name,
       z.level,
       z.kind,
       round(z.area_m2) AS area_m2
FROM geo.zones z
WHERE ST_Covers(z.geom, ST_SetSRID(ST_MakePoint(@lon, @lat), 4326))
ORDER BY z.area_m2
LIMIT 50`

const findStreetsSQL = `
WITH q AS (SELECT @nameNorm::text AS s),
matches AS (
  SELECT n.entity_id,
         MAX(CASE WHEN n.name_norm = q.s THEN 3 WHEN n.name_norm LIKE q.s || '%' THEN 2 ELSE 1 END) AS match_rank,
         MAX(similarity(n.name_norm, q.s)) AS sim,
         (ARRAY_AGG(n.name_norm ORDER BY (n.name_norm = q.s) DESC, (n.name_norm LIKE q.s || '%') DESC,
                    similarity(n.name_norm, q.s) DESC))[1] AS best_norm
  FROM geo.names n, q
  WHERE n.entity_kind = 'STREET'
    AND (n.name_norm = q.s OR n.name_norm LIKE q.s || '%' OR n.name_norm % q.s)
  GROUP BY n.entity_id
)
SELECT s.osm_id, s.label, m.best_norm, m.match_rank, m.sim,
       c.label AS city_label,
       ST_Y(ST_PointOnSurface(s.geom)) AS lat, ST_X(ST_PointOnSurface(s.geom)) AS lon
FROM matches m
JOIN geo.streets s ON ('W' || s.osm_id) = m.entity_id
LEFT JOIN LATERAL (
  SELECT z.label, z.area_m2 FROM geo.zones z
  WHERE z.level = 'CITY' AND ST_Covers(z.geom, ST_PointOnSurface(s.geom))
  ORDER BY z.area_m2 LIMIT 1
) c ON true
ORDER BY m.match_rank DESC, m.sim DESC, COALESCE(c.area_m2, 0) DESC, s.osm_id
LIMIT @limit`

const findAreasSQL = `
WITH q AS (SELECT @nameNorm::text AS s),
matches AS (
  SELECT n.entity_id,
         MAX(CASE WHEN n.name_norm = q.s THEN 3 WHEN n.name_norm LIKE q.s || '%' THEN 2 ELSE 1 END) AS match_rank,
         MAX(similarity(n.name_norm, q.s)) AS sim
  FROM geo.names n, q
  WHERE n.entity_kind = 'AREA'
    AND (n.name_norm = q.s OR n.name_norm LIKE q.s || '%' OR n.name_norm % q.s)
  GROUP BY n.entity_id
)
SELECT z.osm_type || z.osm_id AS id, z.label, z.level, z.kind, round(z.area_m2) AS area_m2,
       m.match_rank, m.sim,
       p.label AS parent_label,
       ST_Y(ST_PointOnSurface(z.geom)) AS lat, ST_X(ST_PointOnSurface(z.geom)) AS lon
FROM matches m
JOIN geo.zones z ON (z.osm_type || z.osm_id) = m.entity_id
LEFT JOIN LATERAL (
  SELECT z2.label FROM geo.zones z2
  WHERE z2.level IN ('OBLAST', 'RAION', 'CITY')
    AND z2.level <> z.level
    AND ST_Covers(z2.geom, ST_PointOnSurface(z.geom))
  ORDER BY z2.area_m2 LIMIT 1
) p ON true
ORDER BY m.match_rank DESC, m.sim DESC,
         CASE z.level
           WHEN 'COUNTRY' THEN 1 WHEN 'OBLAST' THEN 2 WHEN 'RAION' THEN 3 WHEN 'CITY' THEN 3
           WHEN 'CITY_DISTRICT' THEN 4 WHEN 'SETTLEMENT' THEN 4
           WHEN 'NEIGHBOURHOOD' THEN 5 WHEN 'QUARTER' THEN 5 ELSE 6
         END,
         z.area_m2 DESC
LIMIT @limit`

const housesSelectSQL = `
SELECT a.osm_type || a.osm_id AS id, a.housenumber,
       c.subtitle,
       round(ST_Area(a.geom::geography)) AS area_m2,
       ST_Y(a.point) AS lat, ST_X(a.point) AS lon
FROM geo.addresses a
LEFT JOIN LATERAL (
  SELECT string_agg(z.label, ', ' ORDER BY z.area_m2 DESC) AS subtitle
  FROM geo.zones z
  WHERE z.level IN ('CITY', 'CITY_DISTRICT') AND ST_Covers(z.geom, a.point)
) c ON true
JOIN geo.streets s ON s.osm_id = @osmID
WHERE a.street_be = s.name_be
  AND ST_DWithin(a.geom, s.geom, 0.02)`

const housesByNumberSQL = housesSelectSQL + `
  AND lower(a.housenumber) = @housenumber
ORDER BY length(a.housenumber), a.housenumber
LIMIT @limit`

const housesFirstSQL = housesSelectSQL + `
ORDER BY length(a.housenumber), a.housenumber
LIMIT @limit`

const findStreetZonesSQL = `
WITH s AS (SELECT geom FROM geo.streets WHERE osm_id = @osmID LIMIT 1)
SELECT z.osm_type || z.osm_id AS id, z.label AS name, z.level, z.kind,
       round(z.area_m2) AS area_m2,
       ST_Y(ST_PointOnSurface(z.geom)) AS lat, ST_X(ST_PointOnSurface(z.geom)) AS lon
FROM geo.zones z, s
WHERE ST_Intersects(z.geom, s.geom)
ORDER BY z.area_m2
LIMIT @limit`

const findAreaSQL = `
SELECT 'ZONE' AS src,
       z.osm_type || z.osm_id AS id,
       z.label AS name,
       z.level,
       z.kind,
       round(z.area_m2) AS area_m2,
       ST_AsGeoJSON(CASE WHEN @exact THEN z.geom ELSE z.geom_display END) AS geojson
FROM geo.zones z
WHERE z.osm_type = @type AND z.osm_id = @osmID
UNION ALL
SELECT 'HOUSE',
       a.osm_type || a.osm_id,
       concat_ws(', ', a.housenumber, COALESCE(s.label, a.street_be)),
       'HOUSE',
       'BUILDING',
       round(ST_Area(a.geom::geography)),
       ST_AsGeoJSON(a.geom)
FROM geo.addresses a
LEFT JOIN LATERAL (
    SELECT label FROM geo.streets st WHERE st.name_be = a.street_be LIMIT 1
) s ON true
WHERE a.osm_type = @type AND a.osm_id = @osmID
LIMIT 1`
