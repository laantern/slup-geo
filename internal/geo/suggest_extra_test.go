package geo

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSuggestRejectsTooLongQuery(t *testing.T) {
	store := &fakeStore{streets: []StreetMatch{streetMatch(1, "бородина", "улица Бородина", 3)}}
	_, err := NewSuggestService(store).Suggest(context.Background(), strings.Repeat("а", maxQueryLength+1))
	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("ошибка = %v, ожидалась ErrInvalidQuery", err)
	}
	if store.streetCalls != 0 {
		t.Fatalf("хранилище не должно вызываться при слишком длинном запросе")
	}
}

func TestSuggestAcceptsMaxLengthQuery(t *testing.T) {
	store := &fakeStore{streets: []StreetMatch{streetMatch(1, "бородина", "улица Бородина", 3)}}
	query := strings.Repeat("а", maxQueryLength)
	if _, err := NewSuggestService(store).Suggest(context.Background(), query); err != nil {
		t.Fatalf("запрос максимальной длины должен приниматься, получено: %v", err)
	}
}

func TestSuggestDeduplicatesNodeAndWayHouse(t *testing.T) {
	// N и W одного адреса (точка и контур дома) дают одинаковую подпись — в списке должен быть один.
	store := &fakeStore{
		streets: []StreetMatch{streetMatch(1, "бородина", "улица Бородина", 3)},
		houses: []HouseMatch{
			houseMatch("W100", "2"),
			houseMatch("N100", "2"),
			houseMatch("W101", "4"),
		},
	}

	items, err := NewSuggestService(store).Suggest(context.Background(), "Бородина 2")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}

	houses := 0
	for _, item := range items {
		if item.Level == LevelHouse {
			houses++
		}
	}
	if houses != 2 {
		t.Fatalf("домов в подсказках = %d (%+v), ожидалось 2", houses, items)
	}
	if items[0].ID != "W100" || items[0].Name != "улица Бородина, д. 2" {
		t.Fatalf("первым должен быть контур дома, получено %+v", items[0])
	}
}
