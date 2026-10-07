package api

import (
	"net/http"
	"os"
	"path/filepath"
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
