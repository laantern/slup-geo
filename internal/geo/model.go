// Package geo — доменный слой гео-сервиса: модели, порты хранилища и логика
// use case'ов (точка, подсказки, границы). SQL живёт в pgstore.go, HTTP — в пакете api.
package geo

import "errors"

// Ошибки уровня use case. HTTP-слой маппит их в коды VALIDATION_ERROR/NOT_FOUND.
var (
	// ErrInvalidCoordinates — координаты вне допустимого диапазона.
	ErrInvalidCoordinates = errors.New("некорректные координаты")
	// ErrInvalidQuery — поисковый запрос короче минимальной длины.
	ErrInvalidQuery = errors.New("некорректный поисковый запрос")
	// ErrInvalidID — идентификатор объекта не распознан.
	ErrInvalidID = errors.New("некорректный идентификатор")
	// ErrNotFound — объект по идентификатору не найден.
	ErrNotFound = errors.New("объект не найден")
)

// ZoneLevel — уровень зоны (значения совпадают с geo.zones.level).
type ZoneLevel string

const (
	LevelHouse         ZoneLevel = "HOUSE"
	LevelResidential   ZoneLevel = "RESIDENTIAL"
	LevelNeighbourhood ZoneLevel = "NEIGHBOURHOOD"
	LevelQuarter       ZoneLevel = "QUARTER"
	LevelCityDistrict  ZoneLevel = "CITY_DISTRICT"
	LevelCity          ZoneLevel = "CITY"
	LevelSettlement    ZoneLevel = "SETTLEMENT"
	LevelRaion         ZoneLevel = "RAION"
	LevelOblast        ZoneLevel = "OBLAST"
	LevelCountry       ZoneLevel = "COUNTRY"
	LevelLanduse       ZoneLevel = "LANDUSE"
	LevelArea          ZoneLevel = "AREA"
)

var zoneLevels = []ZoneLevel{
	LevelHouse, LevelResidential, LevelNeighbourhood, LevelQuarter, LevelCityDistrict,
	LevelCity, LevelSettlement, LevelRaion, LevelOblast, LevelCountry, LevelLanduse, LevelArea,
}

// ZoneLevelFromOSM переводит значение из БД в уровень, неизвестное — AREA.
func ZoneLevelFromOSM(value string) ZoneLevel {
	for _, level := range zoneLevels {
		if string(level) == value {
			return level
		}
	}
	return LevelArea
}

// ZoneKind — вид зоны (значения совпадают с geo.zones.kind).
type ZoneKind string

const (
	KindBuilding ZoneKind = "BUILDING"
	KindLanduse  ZoneKind = "LANDUSE"
	KindPlace    ZoneKind = "PLACE"
	KindAdmin    ZoneKind = "ADMIN"
	KindArea     ZoneKind = "AREA"
)

var zoneKinds = []ZoneKind{KindBuilding, KindLanduse, KindPlace, KindAdmin, KindArea}

// ZoneKindFromOSM переводит значение из БД в вид, неизвестное — AREA.
func ZoneKindFromOSM(value string) ZoneKind {
	for _, kind := range zoneKinds {
		if string(kind) == value {
			return kind
		}
	}
	return KindArea
}

// LocationZone — элемент ответа точки: дом или зона.
type LocationZone struct {
	ID     string
	Name   string
	Level  ZoneLevel
	Kind   ZoneKind
	AreaM2 int64
}

// PointLocation — результат GET /v1/point.
type PointLocation struct {
	Text  *string
	Zones []LocationZone
}

// Suggestion — элемент подсказки GET /v1/suggest.
type Suggestion struct {
	ID       string
	Name     string
	Level    ZoneLevel
	Kind     ZoneKind
	AreaM2   int64
	Subtitle *string
	Lat      float64
	Lon      float64
}

// Area — граница объекта GET /v1/areas/{id}.
type Area struct {
	ID       string
	Name     string
	Level    ZoneLevel
	Kind     ZoneKind
	AreaM2   int64
	Geometry []byte // GeoJSON
}
