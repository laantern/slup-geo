package geo

import (
	"context"
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Параметры подбора подсказок — совпадают с прежней Kotlin-реализацией модуля location.
const (
	suggestResultLimit = 10
	minQueryLength     = 2
	// maxQueryLength — защитный лимит длины запроса: нормализация и триграммы
	// не должны обрабатывать килобайты текста на каждый запрос.
	maxQueryLength    = 128
	streetSearchLimit = 5
	housesLimit       = 3
	zonesLimit        = 5
	adminBonus        = 150
	// houseSearchStreetLimit — окно улиц для перебора при поиске номера дома:
	// у одной улицы много way в разных городах, верхние позиции занимает самый крупный город.
	houseSearchStreetLimit = 50
)

var adminLevels = map[ZoneLevel]struct{}{
	LevelCity:         {},
	LevelRaion:        {},
	LevelCityDistrict: {},
	LevelOblast:       {},
	LevelCountry:      {},
	LevelSettlement:   {},
}

// SuggestService — поисковые подсказки по улицам, зонам и домам.
type SuggestService struct {
	store SearchStore
}

// NewSuggestService собирает сервис подсказок.
func NewSuggestService(store SearchStore) *SuggestService {
	return &SuggestService{store: store}
}

// Suggest возвращает до 10 подсказок: админ-зоны/улицы по сквозному ранжированию,
// затем дома найденной улицы → зоны улицы → оставшиеся зоны по имени.
func (s *SuggestService) Suggest(ctx context.Context, raw string) ([]Suggestion, error) {
	if utf8.RuneCountInString(raw) > maxQueryLength {
		return nil, ErrInvalidQuery
	}
	nameNorm := Canon(raw)
	if utf8.RuneCountInString(nameNorm) < minQueryLength {
		return nil, ErrInvalidQuery
	}

	streets, err := s.store.Streets(ctx, nameNorm, streetSearchLimit)
	if err != nil {
		return nil, fmt.Errorf("поиск улиц: %w", err)
	}
	areas, err := s.store.Areas(ctx, nameNorm, suggestResultLimit)
	if err != nil {
		return nil, fmt.Errorf("поиск зон: %w", err)
	}

	items := make([]Suggestion, 0, suggestResultLimit)
	usedIDs := make(map[string]struct{})

	tokens := strings.Fields(nameNorm)
	expansion := resolveExpansion(streets, tokens)
	streetScore := math.MinInt32
	if expansion != nil {
		streetScore = expansion.rank()
	}

	// Сквозное ранжирование: зоны/районы, которые не слабее найденной улицы, идут первыми
	// («Гомель» — город, а не дома «Гомельского переулка»).
	areaIndex := 0
	for areaIndex < len(areas) && len(items) < suggestResultLimit && areaRank(areas[areaIndex]) >= streetScore {
		appendArea(&items, usedIDs, areas[areaIndex])
		areaIndex++
	}

	if expansion != nil && len(items) < suggestResultLimit {
		houses, houseStreet, err := s.findHouses(ctx, nameNorm, expansion)
		if err != nil {
			return nil, err
		}
		appendHouses(&items, usedIDs, houseStreet.Label, houses)

		if len(items) < suggestResultLimit {
			zones, err := s.store.StreetZones(ctx, houseStreet.OSMID, zonesLimit)
			if err != nil {
				return nil, fmt.Errorf("поиск зон улицы: %w", err)
			}
			appendStreetZones(&items, usedIDs, houseStreet.CityLabel, zones)
		}
	}

	for areaIndex < len(areas) && len(items) < suggestResultLimit {
		appendArea(&items, usedIDs, areas[areaIndex])
		areaIndex++
	}

	return items, nil
}

// findHouses ищет дома с извлечённым номером: сначала у улицы-лидера, а если номера у неё нет —
// перебирает остальные улицы по ранжированию, пропуская повторы «улица + город» (иначе весь
// список кандидатов занимают way одной улицы одного города). Так «14 Полесская» находит дом 14
// на Полесской в городе, где он есть, а не теряет его.
func (s *SuggestService) findHouses(
	ctx context.Context,
	nameNorm string,
	expansion *streetExpansion,
) ([]HouseMatch, StreetMatch, error) {
	houses, err := s.store.StreetHouses(ctx, expansion.street.OSMID, expansion.housenumber, housesLimit)
	if err != nil {
		return nil, StreetMatch{}, fmt.Errorf("поиск домов улицы: %w", err)
	}
	if len(houses) > 0 || expansion.housenumber == nil {
		return houses, expansion.street, nil
	}

	streets, err := s.store.Streets(ctx, nameNorm, houseSearchStreetLimit)
	if err != nil {
		return nil, StreetMatch{}, fmt.Errorf("расширенный поиск улиц: %w", err)
	}

	seen := make(map[string]struct{}, len(streets))
	leaderCity := ""
	if expansion.street.CityLabel != nil {
		leaderCity = *expansion.street.CityLabel
	}
	seen[expansion.street.Label+"|"+leaderCity] = struct{}{}

	for _, street := range streets {
		city := ""
		if street.CityLabel != nil {
			city = *street.CityLabel
		}
		key := street.Label + "|" + city
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		found, err := s.store.StreetHouses(ctx, street.OSMID, expansion.housenumber, housesLimit)
		if err != nil {
			return nil, StreetMatch{}, fmt.Errorf("поиск домов улицы: %w", err)
		}
		if len(found) > 0 {
			return found, street, nil
		}
	}
	return nil, expansion.street, nil
}

func appendHouses(items *[]Suggestion, usedIDs map[string]struct{}, streetLabel string, houses []HouseMatch) {
	// N и W одного и того же адреса (точка и контур дома) дают одинаковую подпись —
	// оставляем только первый вариант (SQL отдаёт контур раньше точки).
	seen := make(map[string]struct{}, len(houses))
	for _, house := range houses {
		if len(*items) >= suggestResultLimit {
			return
		}
		if _, used := usedIDs[house.ID]; used {
			continue
		}

		name := streetLabel
		if house.Number != nil {
			name = streetLabel + ", д. " + *house.Number
		}
		key := name + "|" + derefString(house.Subtitle)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		usedIDs[house.ID] = struct{}{}

		*items = append(*items, Suggestion{
			ID:       house.ID,
			Name:     name,
			Level:    LevelHouse,
			Kind:     KindBuilding,
			AreaM2:   house.AreaM2,
			Subtitle: house.Subtitle,
			Lat:      house.Lat,
			Lon:      house.Lon,
		})
	}
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func appendStreetZones(items *[]Suggestion, usedIDs map[string]struct{}, cityLabel *string, zones []ZoneMatch) {
	for _, zone := range zones {
		if len(*items) >= suggestResultLimit {
			return
		}
		if _, used := usedIDs[zone.ID]; used {
			continue
		}
		usedIDs[zone.ID] = struct{}{}

		*items = append(*items, Suggestion{
			ID:       zone.ID,
			Name:     zone.Name,
			Level:    zone.Level,
			Kind:     zone.Kind,
			AreaM2:   zone.AreaM2,
			Subtitle: cityLabel,
			Lat:      zone.Lat,
			Lon:      zone.Lon,
		})
	}
}

func appendArea(items *[]Suggestion, usedIDs map[string]struct{}, area AreaMatch) {
	if _, used := usedIDs[area.ID]; used {
		return
	}
	usedIDs[area.ID] = struct{}{}

	*items = append(*items, Suggestion{
		ID:       area.ID,
		Name:     area.Name,
		Level:    area.Level,
		Kind:     area.Kind,
		AreaM2:   area.AreaM2,
		Subtitle: area.Subtitle,
		Lat:      area.Lat,
		Lon:      area.Lon,
	})
}

// areaRank — оценка совпадения зоны: ранг (точное/префикс/нечёткое) + похожесть;
// админ-зоны получают бонус, чтобы «Гомель» и «Железнодорожный район» не проигрывали одноимённым улицам.
func areaRank(area AreaMatch) int {
	score := area.MatchRank*100 + int(area.Similarity*10)
	if _, admin := adminLevels[area.Level]; admin {
		score += adminBonus
	}
	return score
}

func streetRank(street StreetMatch) int {
	return street.MatchRank*100 + int(street.Similarity*10)
}

type streetExpansion struct {
	street      StreetMatch
	housenumber *string
	// streetNorm — остаток запроса после извлечения номера дома (для ранга совпадения улицы).
	streetNorm string
}

// rank — сила совпадения улицы для сквозного ранжирования.
// Когда номер дома извлечён из строки, вся строка уже не совпадает с именем улицы,
// поэтому ранг считается по streetNorm (без номера): иначе нечёткие админ-зоны с бонусом
// вытесняют дома — «14 Полесская» должно находить дом 14, а не село «Полесск».
func (e *streetExpansion) rank() int {
	if e.housenumber != nil && e.streetNorm != "" {
		switch {
		case e.street.NameNorm == e.streetNorm:
			return 3*100 + int(e.street.Similarity*10)
		case strings.HasPrefix(e.street.NameNorm, e.streetNorm+" "):
			return 2*100 + int(e.street.Similarity*10)
		}
	}
	return streetRank(e.street)
}

// resolveExpansion реализует правила разбора: (1) вся строка — улица, если её имя содержит
// все токены запроса; (2) иначе цифровой токен (+ буква-суффикс) — номер дома, остальное — улица.
func resolveExpansion(streets []StreetMatch, tokens []string) *streetExpansion {
	if len(streets) == 0 {
		return nil
	}

	for _, street := range streets {
		if street.MatchRank >= 2 && containsAllTokens(street.NameNorm, tokens) {
			return &streetExpansion{street: street}
		}
	}

	numberIndex := -1
	for i, token := range tokens {
		if isNumberToken(token) {
			numberIndex = i // indexOfLast
		}
	}
	if numberIndex >= 0 {
		number, usedIndexes := numberWithSuffix(tokens, numberIndex)
		parts := make([]string, 0, len(tokens))
		for i, token := range tokens {
			if _, used := usedIndexes[i]; !used {
				parts = append(parts, token)
			}
		}
		streetNorm := strings.Join(parts, " ")
		if streetNorm != "" {
			for _, street := range streets {
				if street.NameNorm == streetNorm || strings.HasPrefix(street.NameNorm, streetNorm+" ") {
					return &streetExpansion{street: street, housenumber: &number, streetNorm: streetNorm}
				}
			}
		}
	}

	return &streetExpansion{street: streets[0]}
}

func containsAllTokens(nameNorm string, tokens []string) bool {
	padded := " " + nameNorm + " "
	for _, token := range tokens {
		if !strings.Contains(padded, " "+token+" ") {
			return false
		}
	}
	return true
}

// numberWithSuffix добавляет соседнюю однобуквенную часть к цифровому токену: «6» + «а» = «6а».
func numberWithSuffix(tokens []string, index int) (string, map[int]struct{}) {
	var number strings.Builder
	number.WriteString(tokens[index])
	used := map[int]struct{}{index: {}}

	if next := tokens[index+1:]; len(next) > 0 && utf8.RuneCountInString(next[0]) == 1 {
		r, _ := utf8.DecodeRuneInString(next[0])
		if unicode.IsLetter(r) {
			number.WriteString(next[0])
			used[index+1] = struct{}{}
		}
	}
	return number.String(), used
}

func isNumberToken(token string) bool {
	if token == "" {
		return false
	}
	for _, r := range token {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
