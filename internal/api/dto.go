// Package api — HTTP-слой гео-сервиса: внутренние ручки API, тайлы и health.
// Ручки API рассчитаны только на вызовы из монолита по внутренней docker-сети:
// наружу через nginx выставляется лишь /tiles/*.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/laantern/slup-geo/internal/geo"
)

// Коды ошибок — как в общем ProblemDetail-контракте монолита (RFC 7807).
const (
	codeValidation = "VALIDATION_ERROR"
	codeNotFound   = "NOT_FOUND"
	codeInternal   = "INTERNAL_SERVER_ERROR"
)

var problemTitles = map[string]string{
	codeValidation: "Некорректный запрос",
	codeNotFound:   "Не найдено",
	codeInternal:   "Внутренняя ошибка сервиса",
}

type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
}

func writeProblem(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{Type: code, Title: problemTitles[code], Status: status})
}

func writeJSON(w http.ResponseWriter, status int, value any, log *slog.Logger) {
	payload, err := json.Marshal(value)
	if err != nil {
		log.Error("сериализация ответа", "error", err)
		writeProblem(w, http.StatusInternalServerError, codeInternal)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

// DTO ответов — форма совпадает с публичным контрактом монолита.

type pointResponse struct {
	Text  *string           `json:"text"`
	Zones []locationZoneDTO `json:"zones"`
}

type locationZoneDTO struct {
	ID     string        `json:"id"`
	Name   string        `json:"name"`
	Level  geo.ZoneLevel `json:"level"`
	Kind   geo.ZoneKind  `json:"kind"`
	AreaM2 int64         `json:"areaM2"`
}

type suggestResponse struct {
	Items []suggestionDTO `json:"items"`
}

type suggestionDTO struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Level    geo.ZoneLevel `json:"level"`
	Kind     geo.ZoneKind  `json:"kind"`
	AreaM2   int64         `json:"areaM2"`
	Subtitle *string       `json:"subtitle"`
	Center   centerDTO     `json:"center"`
}

type centerDTO struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type areaResponse struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Level    geo.ZoneLevel   `json:"level"`
	Kind     geo.ZoneKind    `json:"kind"`
	AreaM2   int64           `json:"areaM2"`
	Geometry json.RawMessage `json:"geometry"`
}

func toPointResponse(location geo.PointLocation) pointResponse {
	zones := make([]locationZoneDTO, 0, len(location.Zones))
	for _, zone := range location.Zones {
		zones = append(zones, locationZoneDTO{
			ID:     zone.ID,
			Name:   zone.Name,
			Level:  zone.Level,
			Kind:   zone.Kind,
			AreaM2: zone.AreaM2,
		})
	}
	return pointResponse{Text: location.Text, Zones: zones}
}

func toSuggestResponse(items []geo.Suggestion) suggestResponse {
	out := make([]suggestionDTO, 0, len(items))
	for _, item := range items {
		out = append(out, suggestionDTO{
			ID:       item.ID,
			Name:     item.Name,
			Level:    item.Level,
			Kind:     item.Kind,
			AreaM2:   item.AreaM2,
			Subtitle: item.Subtitle,
			Center:   centerDTO{Lat: item.Lat, Lon: item.Lon},
		})
	}
	return suggestResponse{Items: out}
}

func toAreaResponse(area geo.Area) areaResponse {
	geometry := json.RawMessage(area.Geometry)
	if len(geometry) == 0 {
		geometry = json.RawMessage("null")
	}
	return areaResponse{
		ID:       area.ID,
		Name:     area.Name,
		Level:    area.Level,
		Kind:     area.Kind,
		AreaM2:   area.AreaM2,
		Geometry: geometry,
	}
}
