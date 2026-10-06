package api

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Имена версионных файлов тайлов (совпадают с update: basemap-<время>.pmtiles).
const (
	tilesFilePrefix = "basemap-"
	tilesFileSuffix = ".pmtiles"
)

// tileNamePattern — whitelist имён файлов тайлов: простые имена, без путей.
var tileNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// fontStackPattern / fontRangePattern — допустимые имена шрифтовых стеков и диапазонов глифов.
var (
	fontStackPattern = regexp.MustCompile(`^[A-Za-z0-9 _-]+$`)
	fontRangePattern = regexp.MustCompile(`^\d+-\d+\.pbf$`)
)

// webFiles — статические веб-ассеты: стиль.
// Стиль использует относительные ссылки (/tiles/...), поэтому работает за любым доменом.
var webFiles = map[string]struct{}{
	"style.json": {},
}

// TilesHandler отдаёт тайлы, манифест, стиль, спрайт и глифы.
//
// Версионные файлы тайлов неизменяемы (immutable, для CDN); алиас basemap.pmtiles,
// стиль и спрайт — no-cache, чтобы клиент узнавал об обновлениях.
type TilesHandler struct {
	// Dir — каталог тайлов (basemap-*.pmtiles, tiles.json).
	Dir string
	// WebDir — каталог веб-ассетов (style.json, fonts/).
	WebDir string
	Log    *slog.Logger
}

func (h *TilesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/tiles/")
	if name == "" || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}

	switch {
	case name == "tiles.json":
		h.serveManifest(w, r)
	case name == "basemap.pmtiles":
		h.serveCurrentTiles(w, r)
	case isWebFile(name):
		h.serveWebFile(w, r, name, "no-cache")
	case strings.HasPrefix(name, "fonts/"):
		h.serveFont(w, r, strings.TrimPrefix(name, "fonts/"))
	default:
		h.serveVersionedTiles(w, r, name)
	}
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

// serveCurrentTiles отдаёт алиас basemap.pmtiles — актуальный версионный файл.
// Клиенту не нужно знать имена версий: ссылка стабильна, обновления подхватываются.
func (h *TilesHandler) serveCurrentTiles(w http.ResponseWriter, r *http.Request) {
	paths, err := filepath.Glob(filepath.Join(h.Dir, tilesFilePrefix+"*"+tilesFileSuffix))
	if err != nil || len(paths) == 0 {
		http.NotFound(w, r)
		return
	}
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))

	w.Header().Set("Content-Type", "application/vnd.pmtiles")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, paths[0])
}

// serveVersionedTiles отдаёт конкретный версионный файл (immutable).
func (h *TilesHandler) serveVersionedTiles(w http.ResponseWriter, r *http.Request, name string) {
	if !tileNamePattern.MatchString(name) || !strings.HasSuffix(name, tilesFileSuffix) {
		http.NotFound(w, r)
		return
	}

	path := filepath.Join(h.Dir, name)
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.pmtiles")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, path)
}

// serveFont отдаёт глифы: fonts/<fontstack>/<range>.pbf.
func (h *TilesHandler) serveFont(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || !fontStackPattern.MatchString(parts[0]) || !fontRangePattern.MatchString(parts[1]) {
		http.NotFound(w, r)
		return
	}
	h.serveWebFile(w, r, filepath.Join("fonts", parts[0], parts[1]), "public, max-age=604800")
}

// serveWebFile отдаёт статический файл из каталога веб-ассетов.
func (h *TilesHandler) serveWebFile(w http.ResponseWriter, r *http.Request, name, cacheControl string) {
	path := filepath.Join(h.WebDir, name)
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", cacheControl)
	http.ServeFile(w, r, path)
}

func isWebFile(name string) bool {
	_, ok := webFiles[name]
	return ok
}
