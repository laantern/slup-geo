package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTilesAliasIgnoresDirectory: алиас basemap.pmtiles не должен отдавать каталог
// с подходящим именем (локальная подмена файла каталогом — 404, а не листинг).
func TestTilesAliasIgnoresDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "basemap-20260101T000000Z.pmtiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	handler := newTilesHandler(t, dir)

	recorder := doRequest(t, handler, http.MethodGet, "/tiles/basemap.pmtiles", nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("статус = %d, ожидался 404", recorder.Code)
	}
}

// TestVendorCacheHeader: вендорные модули кэшируются сутки (не неделю), чтобы при апгрейде
// образа браузер не смешивал версии ESM-модулей из долгого кэша.
func TestVendorCacheHeader(t *testing.T) {
	handler := newTilesHandler(t, t.TempDir())
	vendorDir := filepath.Join(handler.WebDir, "vendor")
	if err := os.MkdirAll(vendorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendorDir, "maplibre-gl.mjs"), []byte("export{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	recorder := doRequest(t, handler, http.MethodGet, "/tiles/vendor/maplibre-gl.mjs", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("статус = %d, ожидался 200", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/javascript; charset=utf-8" {
		t.Fatalf("content-type = %q", contentType)
	}
	if cacheControl := recorder.Header().Get("Cache-Control"); cacheControl != "public, max-age=86400" {
		t.Fatalf("cache-control = %q", cacheControl)
	}
}

// TestTilesETag: алиас и версионный файл отдаются с ETag версии — клиент pmtiles по нему
// замечает смену архива после обновления; If-None-Match даёт 304.
func TestTilesETag(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "basemap-20260101T000000Z.pmtiles"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "basemap-20260102T000000Z.pmtiles"), []byte("newer"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := newTilesHandler(t, dir)

	alias := doRequest(t, handler, http.MethodGet, "/tiles/basemap.pmtiles", nil)
	if etag := alias.Header().Get("ETag"); etag != `"basemap-20260102T000000Z-5"` {
		t.Fatalf("ETag алиаса = %q, ожидалась последняя версия", etag)
	}

	versioned := doRequest(t, handler, http.MethodGet, "/tiles/basemap-20260101T000000Z.pmtiles", nil)
	if etag := versioned.Header().Get("ETag"); etag != `"basemap-20260101T000000Z-3"` {
		t.Fatalf("ETag версионного файла = %q", etag)
	}

	recorder := doRequest(t, handler, http.MethodGet, "/tiles/basemap.pmtiles",
		map[string]string{"If-None-Match": `"basemap-20260102T000000Z-5"`})
	if recorder.Code != http.StatusNotModified {
		t.Fatalf("статус с If-None-Match = %d, ожидался 304", recorder.Code)
	}
}

// TestTilesManifestFallbackAttribution: пустой манифест несёт полную атрибуцию карты.
func TestTilesManifestFallbackAttribution(t *testing.T) {
	handler := newTilesHandler(t, t.TempDir())
	recorder := doRequest(t, handler, http.MethodGet, "/tiles/tiles.json", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("статус = %d", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "© OpenMapTiles · © OpenStreetMap contributors") {
		t.Fatalf("в манифесте нет атрибуции OpenMapTiles: %s", body)
	}
}
