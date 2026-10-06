package geo

import (
	"context"
	"errors"
)

// fakeStore реализует все порты хранилища для юнит-тестов.
type fakeStore struct {
	house    *House
	houseErr error
	zones    []ZoneRow
	zonesErr error

	streets        []StreetMatch
	streetsMore    []StreetMatch
	streetsErr     error
	areas          []AreaMatch
	areasErr       error
	houses         []HouseMatch
	housesByOSM    map[int64][]HouseMatch
	housesErr      error
	streetZones    []ZoneMatch
	zonesStreetErr error

	lastHousenumber *string
	houseCalls      int
	houseCallsByOSM []int64
	streetCalls     int

	area     *AreaRow
	areaErr  error
	areaCall int
}

func (f *fakeStore) NearestHouse(context.Context, float64, float64) (*House, error) {
	f.houseCalls++
	if f.houseErr != nil {
		return nil, f.houseErr
	}
	return f.house, nil
}

func (f *fakeStore) ZonesAt(context.Context, float64, float64) ([]ZoneRow, error) {
	if f.zonesErr != nil {
		return nil, f.zonesErr
	}
	return f.zones, nil
}

func (f *fakeStore) Streets(context.Context, string, int) ([]StreetMatch, error) {
	f.streetCalls++
	if f.streetsErr != nil {
		return nil, f.streetsErr
	}
	if f.streetCalls > 1 && f.streetsMore != nil {
		return f.streetsMore, nil
	}
	return f.streets, nil
}

func (f *fakeStore) Areas(context.Context, string, int) ([]AreaMatch, error) {
	if f.areasErr != nil {
		return nil, f.areasErr
	}
	return f.areas, nil
}

func (f *fakeStore) StreetHouses(_ context.Context, osmID int64, housenumber *string, _ int) ([]HouseMatch, error) {
	f.lastHousenumber = housenumber
	f.houseCallsByOSM = append(f.houseCallsByOSM, osmID)
	if f.housesErr != nil {
		return nil, f.housesErr
	}
	if f.housesByOSM != nil {
		return f.housesByOSM[osmID], nil
	}
	return f.houses, nil
}

func (f *fakeStore) StreetZones(context.Context, int64, int) ([]ZoneMatch, error) {
	if f.zonesStreetErr != nil {
		return nil, f.zonesStreetErr
	}
	return f.streetZones, nil
}

func (f *fakeStore) FindArea(context.Context, string, int64, bool) (*AreaRow, error) {
	f.areaCall++
	if f.areaErr != nil {
		return nil, f.areaErr
	}
	return f.area, nil
}

var errStore = errors.New("хранилище недоступно")
