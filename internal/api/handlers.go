package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/laantern/slup-geo/internal/geo"
)

// HealthChecker — проверки для /health: соединение с БД и готовность схемы geo.
type HealthChecker interface {
	Ping(ctx context.Context) error
	SchemaReady(ctx context.Context) (bool, error)
}

// Meta — сведения о сборке и файлах состояния для /health и /status.
type Meta struct {
	StatePath  string
	StatusPath string
	Version    string
}

// Handlers — ручки внутреннего API.
type Handlers struct {
	Points  *geo.PointService
	Suggest *geo.SuggestService
	Areas   *geo.AreaService
	Health  HealthChecker
	Tiles   *TilesHandler
	Meta    Meta
	// ExampleEnabled — отдавать страницу-пример /example.
	ExampleEnabled bool
	Log            *slog.Logger
}

// NewRouter собирает маршруты сервиса: security-заголовки + access-лог вокруг mux.
func NewRouter(h *Handlers) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /status", h.status)
	mux.HandleFunc("GET /v1/point", h.point)
	mux.HandleFunc("GET /v1/suggest", h.suggest)
	mux.HandleFunc("GET /v1/areas/{id}", h.area)
	mux.Handle("GET /tiles/", h.Tiles)
	if h.ExampleEnabled {
		exampleDir := filepath.Join(h.Tiles.WebDir, "example")
		// Регистрируем только "/example/": ServeMux сам редиректит "/example" на него,
		// чтобы относительные пути внутри страницы резолвились верно.
		// no-cache: страница и модули должны обновляться вместе с образом.
		mux.Handle("GET /example/", http.StripPrefix("/example/", noCache(http.FileServer(http.Dir(exampleDir)))))
	}
	return withMiddleware(h.Log, mux)
}

// stateFile — поля state.json, нужные API (пишет update).
type stateFile struct {
	ImportedAt    time.Time  `json:"importedAt"`
	PBF           string     `json:"pbf"`
	PBFBytes      int64      `json:"pbfBytes"`
	Tiles         []tileFile `json:"tiles"`
	SchemaVersion int        `json:"schemaVersion"`
}

type tileFile struct {
	Name      string    `json:"name"`
	SizeBytes int64     `json:"sizeBytes"`
	BuiltAt   time.Time `json:"builtAt"`
}

// statusFile — поля status.json (статус последней попытки обновления).
type statusFile struct {
	State      string     `json:"state"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Error      string     `json:"error,omitempty"`
}

type healthResponse struct {
	Status      string          `json:"status"`
	SchemaReady bool            `json:"schemaReady"`
	ImportedAt  *time.Time      `json:"importedAt,omitempty"`
	LastUpdate  json.RawMessage `json:"lastUpdate,omitempty"`
}

// health — честная проверка: не только ping БД, но и готовность представлений geo.
// 200 — данные на месте; 503 starting/no_data — импорт идёт или данных ещё нет.
func (h *Handlers) health(w http.ResponseWriter, r *http.Request) {
	if h.Health != nil {
		if err := h.Health.Ping(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unavailable"}, h.Log)
			return
		}
		ready, err := h.Health.SchemaReady(r.Context())
		if err != nil {
			h.Log.Error("проверка схемы geo", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unavailable"}, h.Log)
			return
		}
		if !ready {
			status := "no_data"
			if st := h.readStatus(); st != nil && st.State == "running" {
				status = "starting"
			}
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": status, "schemaReady": false}, h.Log)
			return
		}
	}

	response := healthResponse{Status: "ok", SchemaReady: true}
	if state := h.readState(); state != nil {
		response.ImportedAt = &state.ImportedAt
	}
	if raw := h.readStatusRaw(); raw != nil {
		response.LastUpdate = raw
	}
	writeJSON(w, http.StatusOK, response, h.Log)
}

// status — подробное состояние данных и последнего обновления (диагностика).
func (h *Handlers) status(w http.ResponseWriter, r *http.Request) {
	response := map[string]any{"version": h.Meta.Version}
	if h.Health != nil {
		if err := h.Health.Ping(r.Context()); err == nil {
			if ready, err := h.Health.SchemaReady(r.Context()); err == nil {
				response["schemaReady"] = ready
			}
		} else {
			response["database"] = "unavailable"
		}
	}
	if state := h.readState(); state != nil {
		response["data"] = state
	}
	if status := h.readStatus(); status != nil {
		response["lastUpdate"] = status
	}
	writeJSON(w, http.StatusOK, response, h.Log)
}

func (h *Handlers) readState() *stateFile {
	payload, err := os.ReadFile(h.Meta.StatePath)
	if err != nil {
		return nil
	}
	var state stateFile
	if err := json.Unmarshal(payload, &state); err != nil {
		h.Log.Warn("state.json повреждён", "error", err)
		return nil
	}
	return &state
}

func (h *Handlers) readStatus() *statusFile {
	raw := h.readStatusRaw()
	if raw == nil {
		return nil
	}
	var status statusFile
	if err := json.Unmarshal(raw, &status); err != nil {
		return nil
	}
	return &status
}

func (h *Handlers) readStatusRaw() json.RawMessage {
	payload, err := os.ReadFile(h.Meta.StatusPath)
	if err != nil || !json.Valid(payload) {
		return nil
	}
	return json.RawMessage(payload)
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
		h.Log.Error("подсказки", "error", err, "q_len", len(q))
		writeProblem(w, http.StatusInternalServerError, codeInternal)
	default:
		writeJSON(w, http.StatusOK, toSuggestResponse(items), h.Log)
	}
}

func (h *Handlers) area(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var exact bool
	switch simplify := r.URL.Query().Get("simplify"); simplify {
	case "", "display":
		exact = false
	case "none":
		exact = true
	default:
		writeProblem(w, http.StatusBadRequest, codeValidation)
		return
	}

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
