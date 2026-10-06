package geo

import (
	"context"
	"fmt"
)

// PointService — локация точки: дом (если есть) + покрывающие зоны.
type PointService struct {
	store PointStore
}

// NewPointService собирает сервис точки.
func NewPointService(store PointStore) *PointService {
	return &PointService{store: store}
}

// Get возвращает локацию точки: дом первым (в пределах 5 м от контура),
// далее зоны по возрастанию площади; text — первый элемент + город.
func (s *PointService) Get(ctx context.Context, lat, lon float64) (PointLocation, error) {
	if !validCoordinates(lat, lon) {
		return PointLocation{}, ErrInvalidCoordinates
	}

	house, err := s.store.NearestHouse(ctx, lat, lon)
	if err != nil {
		return PointLocation{}, fmt.Errorf("поиск ближайшего дома: %w", err)
	}
	zones, err := s.store.ZonesAt(ctx, lat, lon)
	if err != nil {
		return PointLocation{}, fmt.Errorf("поиск зон точки: %w", err)
	}

	var city *string
	for _, zone := range zones {
		if zone.Level == LevelCity || zone.Level == LevelSettlement {
			name := zone.Name
			city = &name
			break
		}
	}

	locations := make([]LocationZone, 0, len(zones)+1)
	if house != nil {
		locations = append(locations, houseZone(*house))
	}
	for _, zone := range zones {
		locations = append(locations, LocationZone{
			ID:     zone.ID,
			Name:   zone.Name,
			Level:  zone.Level,
			Kind:   zone.Kind,
			AreaM2: zone.AreaM2,
		})
	}

	var text *string
	if len(locations) > 0 {
		value := locations[0].Name
		if city != nil && *city != value {
			value = value + ", " + *city
		}
		text = &value
	}

	return PointLocation{Text: text, Zones: locations}, nil
}

func validCoordinates(lat, lon float64) bool {
	return lat >= -90.0 && lat <= 90.0 && lon >= -180.0 && lon <= 180.0
}

func houseZone(house House) LocationZone {
	name := house.ID
	switch {
	case house.Street != nil && house.Housenumber != nil:
		name = *house.Street + ", д. " + *house.Housenumber
	case house.Housenumber != nil:
		name = "д. " + *house.Housenumber
	case house.Street != nil:
		name = *house.Street
	}
	return LocationZone{
		ID:     house.ID,
		Name:   name,
		Level:  LevelHouse,
		Kind:   KindBuilding,
		AreaM2: house.AreaM2,
	}
}
