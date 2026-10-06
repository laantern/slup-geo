package api

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// tileNamePattern — whitelist имён: только простые имена файлов, без путей.
var tileNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// TilesHandler отдаёт PMTiles и манифест tiles.json из каталога тайлов.
// Файлы версионные (basemap-<version>.pmtiles) — их можно кэшировать бессрочно;
// tiles.json — no-cache, чтобы клиент узнавал о новом файле.
type TilesHandler struct {
	Dir string
	Log *slog.Logger
}

func (h *TilesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/tiles/")
	if name == "" || !tileNamePattern.MatchString(name) {
		http.NotFound(w, r)
		return
	}

	if name == "tiles.json" {
		h.serveManifest(w, r)
		return
	}
	if !strings.HasSuffix(name, ".pmtiles") {
		http.NotFound(w, r)
		return
	}

	path := filepath.Join(h.Dir, name)
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}

	// Versiónный файл неизменяем: Range и Content-Length обрабатывает http.ServeFile.
	w.Header().Set("Content-Type", "application/vnd.pmtiles")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, path)
}

// serveManifest отдаёт tiles.json; пока тайлы не собраны — пустой валидный ответ.
func (h *TilesHandler) serveManifest(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(h.Dir, "tiles.json")
	payload, err := os.ReadFile(path)
	if err != nil {
		h.Log.Warn("tiles.json не найден, отдаём пустой манифест", "error", err)
		payload = []byte(`{"files":[],"attribution":"© OpenStreetMap contributors"}`)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}
