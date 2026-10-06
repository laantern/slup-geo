package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/laantern/slup-geo/internal/geo"
)

// HealthChecker — минимальная проверка живости БД для /health.
type HealthChecker interface {
	Ping(ctx context.Context) error
}

// Handlers — ручки внутреннего API.
type Handlers struct {
	Points  *geo.PointService
	Suggest *geo.SuggestService
	Areas   *geo.AreaService
	Health  HealthChecker
	Tiles   *TilesHandler
	// ExampleEnabled — отдавать страницу-пример /example.
	ExampleEnabled bool
	Log            *slog.Logger
}

// NewRouter собирает маршруты сервиса.
func NewRouter(h *Handlers) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /v1/point", h.point)
	mux.HandleFunc("GET /v1/suggest", h.suggest)
	mux.HandleFunc("GET /v1/areas/{id}", h.area)
	mux.Handle("GET /tiles/", h.Tiles)
	if h.ExampleEnabled {
		exampleDir := filepath.Join(h.Tiles.WebDir, "example")
		// Регистрируем только "/example/": ServeMux сам редиректит "/example" на него,
		// чтобы относительные пути внутри страницы резолвились верно.
		mux.Handle("GET /example/", http.StripPrefix("/example/", http.FileServer(http.Dir(exampleDir))))
	}
	return mux
}

func (h *Handlers) health(w http.ResponseWriter, r *http.Request) {
	if h.Health != nil {
		if err := h.Health.Ping(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"}, h.Log)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"}, h.Log)
}

func (h *Handlers) point(w http.ResponseWriter, r *http.Request) {
	lat, err := parseFloatQuery(r, "lat")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, codeValidation)
		return
	}
	lon, err := parseFloatQuery(r, "lon")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, codeValidation)
		return
	}

	location, err := h.Points.Get(r.Context(), lat, lon)
	switch {
	case errors.Is(err, geo.ErrInvalidCoordinates):
		writeProblem(w, http.StatusBadRequest, codeValidation)
	case err != nil:
		h.Log.Error("локация точки", "error", err, "lat", lat, "lon", lon)
		writeProblem(w, http.StatusInternalServerError, codeInternal)
	default:
		writeJSON(w, http.StatusOK, toPointResponse(location), h.Log)
	}
}

func (h *Handlers) suggest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")

	items, err := h.Suggest.Suggest(r.Context(), q)
	switch {
	case errors.Is(err, geo.ErrInvalidQuery):
		writeProblem(w, http.StatusBadRequest, codeValidation)
	case err != nil:
		h.Log.Error("подсказки", "error", err, "q", q)
		writeProblem(w, http.StatusInternalServerError, codeInternal)
	default:
		writeJSON(w, http.StatusOK, toSuggestResponse(items), h.Log)
	}
}

func (h *Handlers) area(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	exact := r.URL.Query().Get("simplify") == "none"

	area, err := h.Areas.Get(r.Context(), id, exact)
	switch {
	case errors.Is(err, geo.ErrInvalidID):
		writeProblem(w, http.StatusBadRequest, codeValidation)
	case errors.Is(err, geo.ErrNotFound):
		writeProblem(w, http.StatusNotFound, codeNotFound)
	case err != nil:
		h.Log.Error("границы объекта", "error", err, "id", id)
		writeProblem(w, http.StatusInternalServerError, codeInternal)
	default:
		writeJSON(w, http.StatusOK, toAreaResponse(area), h.Log)
	}
}

func parseFloatQuery(r *http.Request, name string) (float64, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, errors.New("параметр не задан")
	}
	return strconv.ParseFloat(raw, 64)
}
