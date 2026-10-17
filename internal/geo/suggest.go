package geo

import (
	"context"
	"fmt"
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
	// houseSearchStreetLimit — окно улиц для перебора при поиске номера дома:
	// у одной улицы много way в разных городах, верхние позиции занимает самый крупный город.
	houseSearchStreetLimit = 50
)

// cityAdminLevels — админ-уровни городского масштаба: их подсказки держатся сразу за домами
// найденной улицы, выше селений и микрорайонов («советск» → Советский район Гомеля/Минска).
var cityAdminLevels = map[ZoneLevel]struct{}{
	LevelCity:         {},
	LevelRaion:        {},
	LevelCityDistrict: {},
	LevelOblast:       {},
	LevelCountry:      {},
}

// SuggestService — поисковые подсказки по улицам, зонам и домам.
type SuggestService struct {
	store SearchStore
}

// NewSuggestService собирает сервис подсказок.
func NewSuggestService(store SearchStore) *SuggestService {
	return &SuggestService{store: store}
}

// Suggest возвращает до 10 подсказок — только совпадения по имени: зоны точнее найденной
// улицы → дома улицы → городские админ-зоны → остальные зоны (точность → масштаб места).
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

	// Сквозное ранжирование: зоны, совпавшие точнее найденной улицы, идут первыми
	// («Гомель» — город, а не дома «Гомельского переулка»).
	areaIndex := 0
	for areaIndex < len(areas) && len(items) < suggestResultLimit && areaBeforeStreet(areas[areaIndex], expansion) {
		appendArea(&items, usedIDs, areas[areaIndex])
		areaIndex++
	}

	if expansion != nil && len(items) < suggestResultLimit {
		houses, houseStreet, err := s.findHouses(ctx, nameNorm, expansion)
		if err != nil {
			return nil, err
		}
		appendHouses(&items, usedIDs, houseStreet.Label, houses)

		// Городские админ-зоны держатся сразу за домами, выше селений и микрорайонов:
		// «советск» — Советская улица, затем Советский район, затем остальные зоны.
		for areaIndex < len(areas) && len(items) < suggestResultLimit && cityAreaAfterHouses(areas[areaIndex], expansion) {
			appendArea(&items, usedIDs, areas[areaIndex])
			areaIndex++
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

// areaBeforeStreet — зона совпала точнее улицы: ранг выше либо, при равном ранге, похожесть
// выше. При извлечённом номере дома («14 Полесская») улица ранжируется точной и зоны её не обгоняют.
func areaBeforeStreet(area AreaMatch, street *streetExpansion) bool {
	if street == nil {
		return true
	}
	rank := street.rank()
	if area.MatchRank != rank {
		return area.MatchRank > rank
	}
	return area.Similarity > street.street.Similarity
}

// cityAreaAfterHouses — городская админ-зона (город, район, область) отстала от улицы
// не более чем на ранг и идёт сразу за её домами: «советск» → Советский район выше селений.
func cityAreaAfterHouses(area AreaMatch, street *streetExpansion) bool {
	if _, city := cityAdminLevels[area.Level]; !city {
		return false
	}
	return area.MatchRank >= street.rank()-1
}

type streetExpansion struct {
	street      StreetMatch
	housenumber *string
	// streetNorm — остаток запроса после извлечения номера дома (для ранга совпадения улицы).
	streetNorm string
}

// rank — ранг совпадения улицы для сквозного ранжирования.
// Когда номер дома извлечён из строки, вся строка уже не совпадает с именем улицы,
// поэтому ранг считается по streetNorm (без номера): иначе городские админ-зоны
// вытесняют дома — «14 Полесская» должно находить дом 14, а не село «Полесск».
func (e *streetExpansion) rank() int {
	if e.housenumber != nil && e.streetNorm != "" {
		switch {
		case e.street.NameNorm == e.streetNorm:
			return 3
		case strings.HasPrefix(e.street.NameNorm, e.streetNorm+" "):
			return 2
		}
	}
	return e.street.MatchRank
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

	tokens = expandGluedHouseMarkers(tokens)

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
			if _, used := usedIndexes[i]; used {
				continue
			}
			if _, marker := houseMarkers[token]; marker {
				// Служебное слово перед номером («Бородина, д. 6А») — не часть названия улицы.
				continue
			}
			parts = append(parts, token)
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

// houseMarkers — служебные слова перед номером дома: «Бородина, д. 6А» и «Бородина дом 6А»
// должны находить дом так же, как «Бородина 6А».
var houseMarkers = map[string]struct{}{"д": {}, "дом": {}}

// expandGluedHouseMarkers разделяет маркер и номер, склеенные без пробела: канон «д6А»
// даёт токен «д6», а разбору нужны отдельные «д» и «6».
func expandGluedHouseMarkers(tokens []string) []string {
	expanded := make([]string, 0, len(tokens)+1)
	for _, token := range tokens {
		rest, ok := strings.CutPrefix(token, "д")
		if !ok || !isNumberToken(rest) {
			expanded = append(expanded, token)
			continue
		}
		expanded = append(expanded, "д", rest)
	}
	return expanded
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
