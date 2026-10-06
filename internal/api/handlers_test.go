package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laantern/slup-geo/internal/geo"
)

func strPtr(value string) *string { return &value }

type fakeStore struct {
	house    *geo.House
	zones    []geo.ZoneRow
	pointErr error

	streets   []geo.StreetMatch
	houses    []geo.HouseMatch
	areas     []geo.AreaMatch
	searchErr error

	areaRow *geo.AreaRow
	areaErr error

	healthErr error
}

func (f *fakeStore) NearestHouse(context.Context, float64, float64) (*geo.House, error) {
	return f.house, f.pointErr
}

func (f *fakeStore) ZonesAt(context.Context, float64, float64) ([]geo.ZoneRow, error) {
	return f.zones, f.pointErr
}

func (f *fakeStore) Streets(context.Context, string, int) ([]geo.StreetMatch, error) {
	return f.streets, f.searchErr
}

func (f *fakeStore) Areas(context.Context, string, int) ([]geo.AreaMatch, error) {
	return f.areas, f.searchErr
}

func (f *fakeStore) StreetHouses(context.Context, int64, *string, int) ([]geo.HouseMatch, error) {
	return f.houses, f.searchErr
}

func (f *fakeStore) StreetZones(context.Context, int64, int) ([]geo.ZoneMatch, error) {
	return nil, f.searchErr
}

func (f *fakeStore) FindArea(context.Context, string, int64, bool) (*geo.AreaRow, error) {
	return f.areaRow, f.areaErr
}

func (f *fakeStore) Ping(context.Context) error { return f.healthErr }

func newTestRouter(t *testing.T, store *fakeStore, tilesDir string) *http.ServeMux {
	t.Helper()
	return newTestRouterWithWeb(t, store, tilesDir, t.TempDir(), true)
}

func newTestRouterWithWeb(t *testing.T, store *fakeStore, tilesDir, webDir string, exampleEnabled bool) *http.ServeMux {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(&Handlers{
		Points:  geo.NewPointService(store),
		Suggest: geo.NewSuggestService(store),
		Areas:   geo.NewAreaService(store),
		Health:  store,
		Tiles: &TilesHandler{
			Dir:    tilesDir,
			WebDir: webDir,
			Log:    log,
		},
		ExampleEnabled: exampleEnabled,
		Log:            log,
	})
}

func doRequest(t *testing.T, router http.Handler, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestPointEndpoint(t *testing.T) {
	router := newTestRouter(t, &fakeStore{
		house: &geo.House{ID: "W1", Housenumber: strPtr("22"), Street: strPtr("улица Григория Денисенко"), AreaM2: 1460},
		zones: []geo.ZoneRow{{ID: "W2", Name: "Гомель", Level: geo.LevelCity, Kind: geo.KindAdmin, AreaM2: 100}},
	}, t.TempDir())

	recorder := doRequest(t, router, http.MethodGet, "/v1/point?lat=52.4&lon=31.0", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("статус = %d, тело: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Text  *string `json:"text"`
		Zones []struct {
			Name  string `json:"name"`
			Level string `json:"level"`
			Kind  string `json:"kind"`
		} `json:"zones"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if response.Text == nil || *response.Text != "улица Григория Денисенко, д. 22, Гомель" {
		t.Fatalf("text = %v", response.Text)
	}
	if len(response.Zones) != 2 || response.Zones[0].Level != "HOUSE" || response.Zones[0].Kind != "BUILDING" {
		t.Fatalf("зоны = %+v", response.Zones)
	}
}

func TestPointEndpointValidation(t *testing.T) {
	router := newTestRouter(t, &fakeStore{}, t.TempDir())

	for _, target := range []string{"/v1/point?lon=31.0", "/v1/point?lat=abc&lon=31.0", "/v1/point?lat=200&lon=31.0"} {
		recorder := doRequest(t, router, http.MethodGet, target, nil)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: статус = %d, ожидался 400", target, recorder.Code)
		}
		if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "problem+json") {
			t.Fatalf("%s: content-type = %s", target, contentType)
		}
	}
}

func TestPointEndpointStoreFailure(t *testing.T) {
	router := newTestRouter(t, &fakeStore{pointErr: errors.New("db down")}, t.TempDir())

	recorder := doRequest(t, router, http.MethodGet, "/v1/point?lat=52.4&lon=31.0", nil)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("статус = %d, ожидался 500", recorder.Code)
	}
}

func TestSuggestEndpoint(t *testing.T) {
	router := newTestRouter(t, &fakeStore{
		streets: []geo.StreetMatch{{
			OSMID: 1, Label: "улица Бородина", NameNorm: "бородина", MatchRank: 3, Similarity: 1.0,
			Lat: 52.4, Lon: 31.0,
		}},
		houses: []geo.HouseMatch{{
			ID: "W100", Number: strPtr("6А"), AreaM2: 1460, Lat: 52.39, Lon: 30.96,
		}},
	}, t.TempDir())

	recorder := doRequest(t, router, http.MethodGet, "/v1/suggest?q=%D0%91%D0%BE%D1%80%D0%BE%D0%B4%D0%B8%D0%BD%D0%B0", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("статус = %d, тело: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Items []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Center struct {
				Lat float64 `json:"lat"`
				Lon float64 `json:"lon"`
			} `json:"center"`
		} `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if len(response.Items) != 1 || response.Items[0].Name != "улица Бородина, д. 6А" {
		t.Fatalf("items = %+v", response.Items)
	}
	if response.Items[0].Center.Lat != 52.39 || response.Items[0].Center.Lon != 30.96 {
		t.Fatalf("center = %+v", response.Items[0].Center)
	}
}

func TestSuggestEndpointShortQuery(t *testing.T) {
	router := newTestRouter(t, &fakeStore{}, t.TempDir())

	recorder := doRequest(t, router, http.MethodGet, "/v1/suggest?q=%D1%8F", nil)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("статус = %d, ожидался 400", recorder.Code)
	}
}

func TestAreaEndpoint(t *testing.T) {
	router := newTestRouter(t, &fakeStore{areaRow: &geo.AreaRow{
		ID: "W-1", Name: "Советский район", Level: geo.LevelCityDistrict, Kind: geo.KindAdmin,
		AreaM2: 50_910_791, Geometry: []byte(`{"type":"Polygon","coordinates":[]}`),
	}}, t.TempDir())

	recorder := doRequest(t, router, http.MethodGet, "/v1/areas/W-1?simplify=display", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("статус = %d, тело: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Geometry json.RawMessage `json:"geometry"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if !strings.Contains(string(response.Geometry), "Polygon") {
		t.Fatalf("geometry = %s", response.Geometry)
	}
}

func TestAreaEndpointErrors(t *testing.T) {
	router := newTestRouter(t, &fakeStore{}, t.TempDir())

	if recorder := doRequest(t, router, http.MethodGet, "/v1/areas/X1", nil); recorder.Code != http.StatusBadRequest {
		t.Fatalf("невалидный id: статус = %d, ожидался 400", recorder.Code)
	}
	if recorder := doRequest(t, router, http.MethodGet, "/v1/areas/W1", nil); recorder.Code != http.StatusNotFound {
		t.Fatalf("не найдено: статус = %d, ожидался 404", recorder.Code)
	}
}

func TestHealthEndpoint(t *testing.T) {
	router := newTestRouter(t, &fakeStore{}, t.TempDir())
	if recorder := doRequest(t, router, http.MethodGet, "/health", nil); recorder.Code != http.StatusOK {
		t.Fatalf("статус = %d, ожидался 200", recorder.Code)
	}

	router = newTestRouter(t, &fakeStore{healthErr: errors.New("db down")}, t.TempDir())
	if recorder := doRequest(t, router, http.MethodGet, "/health", nil); recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("статус = %d, ожидался 503", recorder.Code)
	}
}

func TestTilesRangeAndHeaders(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("PMTiles-test-payload")
	name := "basemap-20261006T120000Z.pmtiles"
	if err := os.WriteFile(filepath.Join(dir, name), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	router := newTestRouter(t, &fakeStore{}, dir)

	recorder := doRequest(t, router, http.MethodGet, "/tiles/"+name, map[string]string{"Range": "bytes=0-6"})
	if recorder.Code != http.StatusPartialContent {
		t.Fatalf("статус = %d, ожидался 206", recorder.Code)
	}
	if recorder.Body.String() != "PMTiles" {
		t.Fatalf("тело = %q", recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/vnd.pmtiles" {
		t.Fatalf("content-type = %s", contentType)
	}
	if cacheControl := recorder.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "immutable") {
		t.Fatalf("cache-control = %s", cacheControl)
	}
	if contentRange := recorder.Header().Get("Content-Range"); !strings.HasPrefix(contentRange, "bytes 0-6/") {
		t.Fatalf("content-range = %s", contentRange)
	}
}

func TestTilesWhitelist(t *testing.T) {
	dir := t.TempDir()
	handler := &TilesHandler{Dir: dir, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	// Проверяем сам обработчик: whitelist имён, без путей и посторонних расширений.
	for _, path := range []string{
		"/tiles/",
		"/tiles/../state.json",
		"/tiles/a/b.pmtiles",
		"/tiles/secret.txt",
		"/tiles/no%20spaces.pmtiles",
	} {
		request := httptest.NewRequest(http.MethodGet, "/tiles/x", nil)
		request.URL.Path = path
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s: статус = %d, ожидался 404", path, recorder.Code)
		}
	}

	// Через роутер: несуществующий простой файл — 404.
	router := newTestRouter(t, &fakeStore{}, dir)
	if recorder := doRequest(t, router, http.MethodGet, "/tiles/nofile.pmtiles", nil); recorder.Code != http.StatusNotFound {
		t.Fatalf("статус = %d, ожидался 404", recorder.Code)
	}
}

func TestTilesManifestFallbackAndFile(t *testing.T) {
	dir := t.TempDir()
	router := newTestRouter(t, &fakeStore{}, dir)

	recorder := doRequest(t, router, http.MethodGet, "/tiles/tiles.json", nil)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"files":[]`) {
		t.Fatalf("фолбэк: статус = %d, тело = %s", recorder.Code, recorder.Body.String())
	}
	if cacheControl := recorder.Header().Get("Cache-Control"); cacheControl != "no-cache" {
		t.Fatalf("cache-control = %s", cacheControl)
	}

	manifest := `{"files":[{"name":"basemap-1.pmtiles","sizeBytes":10,"builtAt":"2026-10-06T12:00:00Z"}],"attribution":"© OpenStreetMap"}`
	if err := os.WriteFile(filepath.Join(dir, "tiles.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	recorder = doRequest(t, router, http.MethodGet, "/tiles/tiles.json", nil)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "basemap-1.pmtiles") {
		t.Fatalf("манифест: статус = %d, тело = %s", recorder.Code, recorder.Body.String())
	}
}

func newTilesHandler(t *testing.T, tilesDir string) *TilesHandler {
	t.Helper()
	return &TilesHandler{
		Dir:    tilesDir,
		WebDir: t.TempDir(),
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestStyleEndpointServesRelativeStyle(t *testing.T) {
	handler := newTilesHandler(t, t.TempDir())
	style := `{"sources":{"openmaptiles":{"url":"pmtiles:///tiles/basemap.pmtiles"}},` +
		`"glyphs":"/tiles/fonts/{fontstack}/{range}.pbf","sprite":"/tiles/sprite"}`
	if err := os.WriteFile(filepath.Join(handler.WebDir, "style.json"), []byte(style), 0o644); err != nil {
		t.Fatal(err)
	}

	recorder := doRequest(t, handler, http.MethodGet, "/tiles/style.json", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("статус = %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "pmtiles:///tiles/basemap.pmtiles") {
		t.Fatalf("нет относительного источника: %s", body)
	}
	if cacheControl := recorder.Header().Get("Cache-Control"); cacheControl != "no-cache" {
		t.Fatalf("cache-control = %s", cacheControl)
	}
}

func TestTilesAliasServesCurrentFile(t *testing.T) {
	dir := t.TempDir()
	oldPayload := []byte("PMTiles-old-payload")
	newPayload := []byte("PMTiles-new-payload")
	if err := os.WriteFile(filepath.Join(dir, "basemap-20260101T000000Z.pmtiles"), oldPayload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "basemap-20261006T120000Z.pmtiles"), newPayload, 0o644); err != nil {
		t.Fatal(err)
	}
	handler := newTilesHandler(t, dir)

	recorder := doRequest(t, handler, http.MethodGet, "/tiles/basemap.pmtiles", map[string]string{"Range": "bytes=0-11"})
	if recorder.Code != http.StatusPartialContent {
		t.Fatalf("статус = %d, ожидался 206", recorder.Code)
	}
	if recorder.Body.String() != "PMTiles-new-" {
		t.Fatalf("отдан не актуальный файл: %q", recorder.Body.String())
	}
	if cacheControl := recorder.Header().Get("Cache-Control"); cacheControl != "no-cache" {
		t.Fatalf("cache-control = %s", cacheControl)
	}
}

func TestFontsAndSpriteEndpoints(t *testing.T) {
	handler := newTilesHandler(t, t.TempDir())
	fontDir := filepath.Join(handler.WebDir, "fonts", "Noto Sans Regular")
	if err := os.MkdirAll(fontDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fontDir, "0-255.pbf"), []byte{0x0A, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(handler.WebDir, "sprite.json"), []byte(`{"sprite":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if recorder := doRequest(t, handler, http.MethodGet, "/tiles/fonts/Noto%20Sans%20Regular/0-255.pbf", nil); recorder.Code != http.StatusOK {
		t.Fatalf("глифы: статус = %d", recorder.Code)
	}
	if recorder := doRequest(t, handler, http.MethodGet, "/tiles/sprite.json", nil); recorder.Code != http.StatusOK {
		t.Fatalf("спрайт: статус = %d", recorder.Code)
	}
	for _, target := range []string{
		"/tiles/fonts/Noto%20Sans%20Regular/x.pbf",
		"/tiles/fonts/Bad!Stack/0-255.pbf",
		"/tiles/fonts/../../../etc/passwd",
		"/tiles/sprite.bmp",
	} {
		if recorder := doRequest(t, handler, http.MethodGet, target, nil); recorder.Code != http.StatusNotFound {
			t.Fatalf("%s: статус = %d, ожидался 404", target, recorder.Code)
		}
	}
}

func TestExamplePage(t *testing.T) {
	webDir := t.TempDir()
	exampleDir := filepath.Join(webDir, "example")
	if err := os.MkdirAll(exampleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	page := `<script src="./maplibre-gl.js"></script>slup-geo example`
	if err := os.WriteFile(filepath.Join(exampleDir, "index.html"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	router := newTestRouterWithWeb(t, &fakeStore{}, t.TempDir(), webDir, true)

	recorder := doRequest(t, router, http.MethodGet, "/example", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("статус = %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "maplibre-gl.js") {
		t.Fatalf("страница не отдана: %s", recorder.Body.String())
	}
}

func TestExampleDisabled(t *testing.T) {
	router := newTestRouterWithWeb(t, &fakeStore{}, t.TempDir(), t.TempDir(), false)

	if recorder := doRequest(t, router, http.MethodGet, "/example", nil); recorder.Code != http.StatusNotFound {
		t.Fatalf("статус = %d, ожидался 404", recorder.Code)
	}
}
