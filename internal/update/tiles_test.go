package update

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/laantern/slup-geo/internal/config"
)

// TestPublishTilesKeepsPrevious: после публикации остаются новая и одна предыдущая версия
// (клиенты с закешированным версионным URL не получают 404), более старые удаляются;
// манифест ссылается только на новую и несёт полную атрибуцию.
func TestPublishTilesKeepsPrevious(t *testing.T) {
	dataDir := t.TempDir()
	u := &Updater{
		cfg: config.Config{DataDir: dataDir},
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if err := os.MkdirAll(u.cfg.TilesDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"basemap-20260101T000000Z.pmtiles", "basemap-20260102T000000Z.pmtiles"} {
		if err := os.WriteFile(filepath.Join(u.cfg.TilesDir(), name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(u.cfg.TmpDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	built := &builtTiles{
		tmpPath: filepath.Join(u.cfg.TmpDir(), "basemap-20260103T000000Z.pmtiles"),
		name:    "basemap-20260103T000000Z.pmtiles",
	}
	if err := os.WriteFile(built.tmpPath, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := u.publishTiles(built)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != built.name {
		t.Fatalf("опубликованные файлы = %+v", files)
	}

	left, err := filepath.Glob(filepath.Join(u.cfg.TilesDir(), "basemap-*.pmtiles"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(left)
	want := []string{"basemap-20260102T000000Z.pmtiles", "basemap-20260103T000000Z.pmtiles"}
	if len(left) != len(want) || filepath.Base(left[0]) != want[0] || filepath.Base(left[1]) != want[1] {
		t.Fatalf("на диске = %v, ожидалось %v", left, want)
	}

	payload, err := os.ReadFile(filepath.Join(u.cfg.TilesDir(), "tiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest tilesManifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Attribution != "© OpenMapTiles · © OpenStreetMap contributors" {
		t.Fatalf("атрибуция манифеста = %q", manifest.Attribution)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Name != built.name {
		t.Fatalf("манифест = %+v", manifest.Files)
	}
}
