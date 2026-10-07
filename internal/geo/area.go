package geo

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// AreaRef — разобранный идентификатор объекта: W664945556 / N123 / W-3628814.
type AreaRef struct {
	Type  string
	OSMID int64
}

var areaRefTypes = map[string]struct{}{"W": {}, "N": {}, "R": {}}

// ParseAreaRef разбирает идентификатор вида W664945556 (relations хранятся с отрицательным id).
// После буквы типа допускаются только цифры (возможно, с ведущим минусом): «W+123» отклоняется.
func ParseAreaRef(id string) (AreaRef, error) {
	if id == "" {
		return AreaRef{}, ErrInvalidID
	}
	areaType := strings.ToUpper(id[:1])
	if _, ok := areaRefTypes[areaType]; !ok {
		return AreaRef{}, ErrInvalidID
	}

	rest := id[1:]
	digits := rest
	if strings.HasPrefix(rest, "-") {
		digits = rest[1:]
	}
	if digits == "" {
		return AreaRef{}, ErrInvalidID
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return AreaRef{}, ErrInvalidID
		}
	}

	osmID, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		return AreaRef{}, ErrInvalidID
	}
	return AreaRef{Type: areaType, OSMID: osmID}, nil
}

// AreaService — границы зон и домов для отрисовки.
type AreaService struct {
	store AreaStore
}

// NewAreaService собирает сервис границ.
func NewAreaService(store AreaStore) *AreaService {
	return &AreaService{store: store}
}

// Get возвращает зону или дом по id; exact — точная геометрия вместо упрощённой.
func (s *AreaService) Get(ctx context.Context, id string, exact bool) (Area, error) {
	ref, err := ParseAreaRef(id)
	if err != nil {
		return Area{}, err
	}

	row, err := s.store.FindArea(ctx, ref.Type, ref.OSMID, exact)
	if err != nil {
		return Area{}, fmt.Errorf("поиск границы %s: %w", id, err)
	}
	if row == nil {
		return Area{}, ErrNotFound
	}

	return Area{
		ID:       row.ID,
		Name:     row.Name,
		Level:    row.Level,
		Kind:     row.Kind,
		AreaM2:   row.AreaM2,
		Geometry: row.Geometry,
	}, nil
}
