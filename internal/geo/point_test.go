package geo

import (
	"context"
	"errors"
	"testing"
)

func strPtr(value string) *string { return &value }

func TestPointGetHouseFirstAndTextWithCity(t *testing.T) {
	store := &fakeStore{
		house: &House{
			ID:          "W664945556",
			Housenumber: strPtr("22"),
			Street:      strPtr("улица Григория Денисенко"),
			AreaM2:      1460,
		},
		zones: []ZoneRow{
			{ID: "W-1", Name: "Шведская горка", Level: LevelResidential, Kind: KindLanduse, AreaM2: 456_973},
			{ID: "W-2", Name: "Гомель", Level: LevelCity, Kind: KindAdmin, AreaM2: 140_058_028},
		},
	}

	location, err := NewPointService(store).Get(context.Background(), 52.3955063, 30.9607991)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}

	if location.Text == nil || *location.Text != "улица Григория Денисенко, д. 22, Гомель" {
		t.Fatalf("текст = %v, ожидался «улица Григория Денисенко, д. 22, Гомель»", location.Text)
	}
	if len(location.Zones) != 3 {
		t.Fatalf("зон = %d, ожидалось 3", len(location.Zones))
	}
	first := location.Zones[0]
	if first.Level != LevelHouse || first.Kind != KindBuilding || first.Name != "улица Григория Денисенко, д. 22" {
		t.Fatalf("первый элемент должен быть домом, получено %+v", first)
	}
	if location.Zones[1].Name != "Шведская горка" {
		t.Fatalf("после дома должна идти зона, получено %+v", location.Zones[1])
	}
}

func TestPointGetWithoutHouseTextFromFirstZone(t *testing.T) {
	store := &fakeStore{
		zones: []ZoneRow{
			{ID: "W-1", Name: "Шведская горка", Level: LevelResidential, Kind: KindLanduse, AreaM2: 456_973},
			{ID: "W-2", Name: "Гомель", Level: LevelCity, Kind: KindAdmin, AreaM2: 140_058_028},
		},
	}

	location, err := NewPointService(store).Get(context.Background(), 52.3981186, 30.9622666)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if location.Text == nil || *location.Text != "Шведская горка, Гомель" {
		t.Fatalf("текст = %v, ожидался «Шведская горка, Гомель»", location.Text)
	}
	if len(location.Zones) != 2 {
		t.Fatalf("зон = %d, ожидалось 2", len(location.Zones))
	}
}

func TestPointGetDoesNotDuplicateCity(t *testing.T) {
	store := &fakeStore{zones: []ZoneRow{{ID: "W-2", Name: "Гомель", Level: LevelCity, Kind: KindAdmin}}}

	location, err := NewPointService(store).Get(context.Background(), 52.4, 31.0)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if location.Text == nil || *location.Text != "Гомель" {
		t.Fatalf("текст = %v, ожидался «Гомель»", location.Text)
	}
}

func TestPointGetEmpty(t *testing.T) {
	location, err := NewPointService(&fakeStore{}).Get(context.Background(), 52.4, 31.0)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if location.Text != nil {
		t.Fatalf("текст = %v, ожидался nil", location.Text)
	}
	if len(location.Zones) != 0 {
		t.Fatalf("зон = %d, ожидалось 0", len(location.Zones))
	}
}

func TestPointGetInvalidCoordinates(t *testing.T) {
	store := &fakeStore{}
	_, err := NewPointService(store).Get(context.Background(), 200.0, 31.0)

	if !errors.Is(err, ErrInvalidCoordinates) {
		t.Fatalf("ошибка = %v, ожидалась ErrInvalidCoordinates", err)
	}
	if store.houseCalls != 0 {
		t.Fatalf("хранилище не должно вызываться при невалидных координатах")
	}
}

func TestPointGetStoreFailure(t *testing.T) {
	store := &fakeStore{houseErr: errStore}
	_, err := NewPointService(store).Get(context.Background(), 52.4, 31.0)
	if err == nil || !errors.Is(err, errStore) {
		t.Fatalf("ошибка = %v, ожидалась обёртка errStore", err)
	}
}
