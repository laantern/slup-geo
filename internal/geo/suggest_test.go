package geo

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func streetMatch(osmID int64, nameNorm, label string, rank int) StreetMatch {
	return StreetMatch{
		OSMID:      osmID,
		Label:      label,
		NameNorm:   nameNorm,
		MatchRank:  rank,
		Similarity: 1.0,
		CityLabel:  strPtr("Гомель"),
		Lat:        52.4,
		Lon:        31.0,
	}
}

func houseMatch(id, number string) HouseMatch {
	return HouseMatch{
		ID:       id,
		Number:   strPtr(number),
		Subtitle: strPtr("Гомель, Центральный район"),
		AreaM2:   1460,
		Lat:      52.39,
		Lon:      30.96,
	}
}

func zoneMatch(id, name string) ZoneMatch {
	return ZoneMatch{
		ID:     id,
		Name:   name,
		Level:  LevelCityDistrict,
		Kind:   KindAdmin,
		AreaM2: 50_910_791,
		Lat:    52.43,
		Lon:    30.96,
	}
}

func areaMatch(id, name string) AreaMatch {
	return AreaMatch{
		ID:         id,
		Name:       name,
		Level:      LevelSettlement,
		Kind:       KindAdmin,
		AreaM2:     100,
		Subtitle:   strPtr("Гомельская область"),
		MatchRank:  1,
		Similarity: 0.6,
		Lat:        52.0,
		Lon:        30.0,
	}
}

// Смещения площади, чтобы компилятор не ругался на неиспользуемую функцию.

func TestSuggestHouseNumberExtracted(t *testing.T) {
	store := &fakeStore{
		streets: []StreetMatch{streetMatch(1, "бородина", "улица Бородина", 3)},
		houses:  []HouseMatch{houseMatch("W100", "6А")},
		streetZones: []ZoneMatch{
			zoneMatch("W-1", "Советский район"),
		},
		areas: []AreaMatch{areaMatch("W-2", "Бородино")},
	}

	items, err := NewSuggestService(store).Suggest(context.Background(), "Бородина 6А")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}

	if store.lastHousenumber == nil || *store.lastHousenumber != "6а" {
		t.Fatalf("номер дома = %v, ожидался «6а»", store.lastHousenumber)
	}
	if len(items) != 3 {
		t.Fatalf("подсказок = %d (%+v), ожидалось 3", len(items), items)
	}
	if items[0].Level != LevelHouse || items[0].Name != "улица Бородина, д. 6А" {
		t.Fatalf("первым должен быть дом, получено %+v", items[0])
	}
	if items[1].Level != LevelCityDistrict || items[1].Name != "Советский район" {
		t.Fatalf("второй должна быть зона улицы, получено %+v", items[1])
	}
	if items[2].Name != "Бородино" {
		t.Fatalf("третьей должна быть зона по имени, получено %+v", items[2])
	}
}

func TestSuggestNumberInsideStreetNameNotSplit(t *testing.T) {
	store := &fakeStore{
		streets: []StreetMatch{streetMatch(1, "3 я линейная", "3-я Линейная улица", 3)},
		houses:  []HouseMatch{houseMatch("W101", "1")},
	}

	items, err := NewSuggestService(store).Suggest(context.Background(), "3я линейная")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if store.lastHousenumber != nil {
		t.Fatalf("номер дома = %v, ожидался nil (вся строка — улица)", *store.lastHousenumber)
	}
	if len(items) == 0 || items[0].Name != "3-я Линейная улица, д. 1" {
		t.Fatalf("ожидался дом 3-й Линейной, получено %+v", items)
	}
}

func TestSuggestOnlyAreas(t *testing.T) {
	store := &fakeStore{areas: []AreaMatch{areaMatch("W-2", "Бородино")}}

	items, err := NewSuggestService(store).Suggest(context.Background(), "Бородино")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(items) != 1 || items[0].Name != "Бородино" {
		t.Fatalf("ожидалась одна зона «Бородино», получено %+v", items)
	}
}

func TestSuggestDeduplicatesZones(t *testing.T) {
	store := &fakeStore{
		streets:     []StreetMatch{streetMatch(1, "бородина", "улица Бородина", 3)},
		streetZones: []ZoneMatch{zoneMatch("W-1", "Советский район")},
		areas:       []AreaMatch{areaMatch("W-1", "Советский район")},
	}

	items, err := NewSuggestService(store).Suggest(context.Background(), "Бородина")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	count := 0
	for _, item := range items {
		if item.ID == "W-1" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("зона W-1 встречается %d раз (%+v), ожидалось 1", count, items)
	}
}

func TestSuggestExactAdminAreaBeforePrefixStreet(t *testing.T) {
	store := &fakeStore{
		streets:     []StreetMatch{streetMatch(1, "гомельский переулок", "Гомельский переулок", 2)},
		houses:      []HouseMatch{houseMatch("W1", "3")},
		streetZones: []ZoneMatch{zoneMatch("W-9", "Бобруйск")},
		areas: []AreaMatch{{
			ID: "W-163244", Name: "Гомель", Level: LevelCity, Kind: KindAdmin,
			MatchRank: 3, Similarity: 1.0, Subtitle: strPtr("Гомельская область"),
		}},
	}

	items, err := NewSuggestService(store).Suggest(context.Background(), "Гомель")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(items) < 2 {
		t.Fatalf("ожидалось минимум 2 подсказки, получено %+v", items)
	}
	if items[0].Name != "Гомель" || items[0].Level != LevelCity {
		t.Fatalf("город должен быть первым, получено %+v", items[0])
	}
	if items[1].Name != "Гомельский переулок, д. 3" {
		t.Fatalf("вторым должен быть дом улицы, получено %+v", items[1])
	}
}

func TestSuggestPrefixRaionBeforeExactStreet(t *testing.T) {
	store := &fakeStore{
		streets: []StreetMatch{streetMatch(1, "железнодорожный", "Железнодорожный переулок", 3)},
		houses:  []HouseMatch{houseMatch("W1", "1")},
		areas: []AreaMatch{{
			ID: "W-3628812", Name: "Железнодорожный район", Level: LevelRaion, Kind: KindAdmin,
			MatchRank: 2, Similarity: 1.0, Subtitle: strPtr("Гомельская область"),
		}},
	}

	items, err := NewSuggestService(store).Suggest(context.Background(), "Железнодорожный")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(items) == 0 || items[0].Level != LevelRaion || items[0].Name != "Железнодорожный район" {
		t.Fatalf("район с бонусом должен быть первым, получено %+v", items)
	}
}

func TestSuggestExactStreetBeforeFuzzyArea(t *testing.T) {
	store := &fakeStore{
		streets: []StreetMatch{streetMatch(1, "бородина", "улица Бородина", 3)},
		houses:  []HouseMatch{houseMatch("W100", "6А")},
		areas:   []AreaMatch{areaMatch("W-2", "Бородино")},
	}

	items, err := NewSuggestService(store).Suggest(context.Background(), "Бородина")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(items) == 0 || items[0].Level != LevelHouse {
		t.Fatalf("точная улица должна обгонять нечёткую зону, получено %+v", items)
	}
}

func TestSuggestHouseNumberBeatsFuzzyAdminArea(t *testing.T) {
	// Реальный кейс «14 Полесская»: улица находится только по триграммам (rank 1),
	// а админ-зоны с бонусом раньше вытесняли дома из выдачи.
	store := &fakeStore{
		streets: []StreetMatch{{
			OSMID: 83201330, Label: "Полесская улица", NameNorm: "полесская",
			MatchRank: 1, Similarity: 0.769, CityLabel: strPtr("Минск"),
			Lat: 53.9, Lon: 27.5,
		}},
		houses: []HouseMatch{{
			ID: "W64749288", Number: strPtr("14"), Subtitle: strPtr("Минск"),
			AreaM2: 500, Lat: 53.87, Lon: 27.5,
		}},
		areas: []AreaMatch{{
			ID: "W-7443288", Name: "Полесск", Level: LevelSettlement, Kind: KindAdmin,
			AreaM2: 91802, Subtitle: strPtr("Ельский район"),
			MatchRank: 1, Similarity: 0.5, Lat: 51.78, Lon: 29.2,
		}},
	}

	for _, query := range []string{"14 Полесская", "Полесская 14"} {
		items, err := NewSuggestService(store).Suggest(context.Background(), query)
		if err != nil {
			t.Fatalf("%s: неожиданная ошибка: %v", query, err)
		}
		if store.lastHousenumber == nil || *store.lastHousenumber != "14" {
			t.Fatalf("%s: номер дома = %v, ожидался «14»", query, store.lastHousenumber)
		}
		if len(items) == 0 || items[0].Level != LevelHouse || !strings.HasPrefix(items[0].Name, "Полесская улица, д. 14") {
			t.Fatalf("%s: дом 14 должен быть первым, получено %+v", query, items)
		}
	}
}

func TestSuggestHouseNumberSearchesOtherStreets(t *testing.T) {
	// Улица-лидер (Минск) нужного номера не имеет: в списке кандидатов — way одной улицы
	// разных городов; фолбэк перебирает города, пропуская повторы «улица + город».
	minsk := StreetMatch{
		OSMID: 1, Label: "Полесская улица", NameNorm: "полесская",
		MatchRank: 1, Similarity: 0.769, CityLabel: strPtr("Минск"), Lat: 53.9, Lon: 27.5,
	}
	brest := StreetMatch{
		OSMID: 2, Label: "Полесская улица", NameNorm: "полесская",
		MatchRank: 1, Similarity: 0.769, CityLabel: strPtr("Брест"), Lat: 52.1, Lon: 23.7,
	}
	gomel := StreetMatch{
		OSMID: 3, Label: "Полесская улица", NameNorm: "полесская",
		MatchRank: 1, Similarity: 0.769, CityLabel: strPtr("Гомель"), Lat: 52.43, Lon: 30.98,
	}
	store := &fakeStore{
		streets:     []StreetMatch{minsk},
		streetsMore: []StreetMatch{minsk, brest, gomel},
		housesByOSM: map[int64][]HouseMatch{
			2: {},
			3: {{
				ID: "W64749288", Number: strPtr("14"), Subtitle: strPtr("Гомель, Железнодорожный район"),
				AreaM2: 500, Lat: 52.43, Lon: 30.99,
			}},
		},
	}

	items, err := NewSuggestService(store).Suggest(context.Background(), "14 Полесская")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}

	wantCalls := []int64{1, 2, 3}
	if len(store.houseCallsByOSM) != len(wantCalls) {
		t.Fatalf("вызовы домов = %v, ожидались %v", store.houseCallsByOSM, wantCalls)
	}
	for i, want := range wantCalls {
		if store.houseCallsByOSM[i] != want {
			t.Fatalf("вызовы домов = %v, ожидались %v", store.houseCallsByOSM, wantCalls)
		}
	}
	if len(items) == 0 || items[0].Level != LevelHouse || items[0].Name != "Полесская улица, д. 14" {
		t.Fatalf("дом 14 должен быть первым, получено %+v", items)
	}
}

func TestSuggestShortQueryRejected(t *testing.T) {
	store := &fakeStore{}
	_, err := NewSuggestService(store).Suggest(context.Background(), "я")

	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("ошибка = %v, ожидалась ErrInvalidQuery", err)
	}
	if store.streetCalls != 0 {
		t.Fatalf("хранилище не должно вызываться при коротком запросе")
	}
}

func TestSuggestStoreFailure(t *testing.T) {
	store := &fakeStore{streetsErr: errStore}
	_, err := NewSuggestService(store).Suggest(context.Background(), "бородина")
	if err == nil || !errors.Is(err, errStore) {
		t.Fatalf("ошибка = %v, ожидалась обёртка errStore", err)
	}
}

func TestSuggestResultLimit(t *testing.T) {
	areas := make([]AreaMatch, 0, 15)
	for i := 0; i < 15; i++ {
		areas = append(areas, areaMatch("W"+string(rune('A'+i)), "Зона"))
	}
	store := &fakeStore{areas: areas}

	items, err := NewSuggestService(store).Suggest(context.Background(), "зона")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(items) != suggestResultLimit {
		t.Fatalf("подсказок = %d, ожидалось %d", len(items), suggestResultLimit)
	}
}
