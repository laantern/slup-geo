package geo

import "context"

// House — ближайший дом к точке (geo.addresses).
type House struct {
	ID          string
	Housenumber *string
	Street      *string
	AreaM2      int64
}

// ZoneRow — зона, накрывающая точку (geo.zones).
type ZoneRow struct {
	ID     string
	Name   string
	Level  ZoneLevel
	Kind   ZoneKind
	AreaM2 int64
}

// PointStore — доступ к данным точки.
type PointStore interface {
	// NearestHouse возвращает дом в пределах 5 м от контура; nil — дома рядом нет.
	NearestHouse(ctx context.Context, lat, lon float64) (*House, error)
	// ZonesAt возвращает зоны, покрывающие точку, по возрастанию площади.
	ZonesAt(ctx context.Context, lat, lon float64) ([]ZoneRow, error)
}

// StreetMatch — совпадение по имени улицы (geo.names + geo.streets).
type StreetMatch struct {
	OSMID      int64
	Label      string
	NameNorm   string
	MatchRank  int
	Similarity float64
	CityLabel  *string
	Lat        float64
	Lon        float64
}

// AreaMatch — совпадение по имени зоны.
type AreaMatch struct {
	ID         string
	Name       string
	Level      ZoneLevel
	Kind       ZoneKind
	AreaM2     int64
	Subtitle   *string
	MatchRank  int
	Similarity float64
	Lat        float64
	Lon        float64
}

// HouseMatch — дом улицы (для подсказок «улица → дома»).
type HouseMatch struct {
	ID       string
	Number   *string
	Subtitle *string
	AreaM2   int64
	Lat      float64
	Lon      float64
}

// ZoneMatch — зона, в которую входит улица.
type ZoneMatch struct {
	ID     string
	Name   string
	Level  ZoneLevel
	Kind   ZoneKind
	AreaM2 int64
	Lat    float64
	Lon    float64
}

// SearchStore — данные для поисковых подсказок.
type SearchStore interface {
	Streets(ctx context.Context, nameNorm string, limit int) ([]StreetMatch, error)
	Areas(ctx context.Context, nameNorm string, limit int) ([]AreaMatch, error)
	StreetHouses(ctx context.Context, streetOSMID int64, housenumber *string, limit int) ([]HouseMatch, error)
	StreetZones(ctx context.Context, streetOSMID int64, limit int) ([]ZoneMatch, error)
}

// AreaRow — строка границы зоны или дома.
type AreaRow struct {
	ID       string
	Name     string
	Level    ZoneLevel
	Kind     ZoneKind
	AreaM2   int64
	Geometry []byte // GeoJSON
}

// AreaStore — доступ к границам.
type AreaStore interface {
	// FindArea ищет зону или дом по типу и osm_id; nil — не найдено.
	FindArea(ctx context.Context, osmType string, osmID int64, exact bool) (*AreaRow, error)
}
