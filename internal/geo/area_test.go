package geo

import (
	"context"
	"errors"
	"testing"
)

func TestParseAreaRef(t *testing.T) {
	cases := []struct {
		name     string
		id       string
		wantType string
		wantID   int64
		wantErr  bool
	}{
		{"way", "W664945556", "W", 664945556, false},
		{"node", "N123", "N", 123, false},
		{"relation с отрицательным id", "W-3628814", "W", -3628814, false},
		{"нижний регистр", "w1", "W", 1, false},
		{"неизвестный тип", "X123", "", 0, true},
		{"нечисловой id", "Wabc", "", 0, true},
		{"пустой", "", "", 0, true},
		{"только тип", "W", "", 0, true},
		{"плюс в id", "W+123", "", 0, true},
		{"пробел в id", "W 123", "", 0, true},
		{"минус без цифр", "W-", "", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref, err := ParseAreaRef(tc.id)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ожидалась ошибка для %q, получено %+v", tc.id, ref)
				}
				return
			}
			if err != nil {
				t.Fatalf("неожиданная ошибка для %q: %v", tc.id, err)
			}
			if ref.Type != tc.wantType || ref.OSMID != tc.wantID {
				t.Fatalf("ref = %+v, ожидалось %s/%d", ref, tc.wantType, tc.wantID)
			}
		})
	}
}

func TestAreaGetFound(t *testing.T) {
	store := &fakeStore{area: &AreaRow{
		ID: "W-3628814", Name: "Советский район", Level: LevelCityDistrict, Kind: KindAdmin,
		AreaM2: 50_910_791, Geometry: []byte(`{"type":"MultiPolygon","coordinates":[]}`),
	}}

	area, err := NewAreaService(store).Get(context.Background(), "W-3628814", false)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if area.Name != "Советский район" || area.Level != LevelCityDistrict || area.Kind != KindAdmin {
		t.Fatalf("неожиданная зона: %+v", area)
	}
}

func TestAreaGetNotFound(t *testing.T) {
	_, err := NewAreaService(&fakeStore{}).Get(context.Background(), "W1", false)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ошибка = %v, ожидалась ErrNotFound", err)
	}
}

func TestAreaGetInvalidIDDoesNotTouchStore(t *testing.T) {
	store := &fakeStore{area: &AreaRow{ID: "W1", Name: "Гомель"}}
	_, err := NewAreaService(store).Get(context.Background(), "X123", false)

	if !errors.Is(err, ErrInvalidID) {
		t.Fatalf("ошибка = %v, ожидалась ErrInvalidID", err)
	}
	if store.areaCall != 0 {
		t.Fatalf("хранилище не должно вызываться при невалидном id")
	}
}

func TestAreaGetStoreFailure(t *testing.T) {
	store := &fakeStore{areaErr: errStore}
	_, err := NewAreaService(store).Get(context.Background(), "W1", false)
	if err == nil || !errors.Is(err, errStore) {
		t.Fatalf("ошибка = %v, ожидалась обёртка errStore", err)
	}
}
