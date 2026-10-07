package update

import (
	"encoding/binary"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/laantern/slup-geo/internal/config"
)

func testConfig(pbfPath string) config.Config {
	return config.Config{PBFPath: pbfPath}
}

func testUpdater(pbfPath string) *Updater {
	return &Updater{
		cfg: testConfig(pbfPath),
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestParsePBFURL(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"https", "https://download.geofabrik.de/europe/belarus-latest.osm.pbf", false},
		{"http localhost", "http://localhost:8080/belarus.pbf", false},
		{"http 127.0.0.1", "http://127.0.0.1/belarus.pbf", false},
		{"http внешний хост", "http://example.com/belarus.pbf", true},
		{"ftp", "ftp://example.com/belarus.pbf", true},
		{"file", "file:///data/osm/belarus.pbf", true},
		{"пусто", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parsePBFURL(tc.raw)
			if tc.wantErr && err == nil {
				t.Fatalf("ожидалась ошибка для %q", tc.raw)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("неожиданная ошибка для %q: %v", tc.raw, err)
			}
		})
	}
}

func TestRedactURL(t *testing.T) {
	got := redactURL("https://user:secret@example.com/belarus.pbf")
	if want := "https://example.com/belarus.pbf"; got != want {
		t.Fatalf("redactURL = %q, ожидалось %q", got, want)
	}
	plain := "https://example.com/belarus.pbf"
	if got := redactURL(plain); got != plain {
		t.Fatalf("redactURL без кредов = %q, ожидалось %q", got, plain)
	}
	if got := redactURL(""); got != "" {
		t.Fatalf("redactURL пустого = %q", got)
	}
}

func pbfHeaderBlob() []byte {
	blob := append([]byte{0x0A, 0x09}, []byte("OSMHeader")...)
	payload := make([]byte, 4+len(blob))
	binary.BigEndian.PutUint32(payload, uint32(len(blob)))
	copy(payload[4:], blob)
	return payload
}

func TestValidatePBF(t *testing.T) {
	dir := t.TempDir()

	good := filepath.Join(dir, "good.pbf")
	if err := os.WriteFile(good, pbfHeaderBlob(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validatePBF(good); err != nil {
		t.Fatalf("валидный PBF отклонён: %v", err)
	}

	html := filepath.Join(dir, "html.pbf")
	if err := os.WriteFile(html, []byte("<!doctype html><html>404</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validatePBF(html); err == nil {
		t.Fatal("HTML-страница не должна проходить валидацию")
	}

	truncated := filepath.Join(dir, "truncated.pbf")
	if err := os.WriteFile(truncated, pbfHeaderBlob()[:6], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validatePBF(truncated); err == nil {
		t.Fatal("обрезанный PBF не должен проходить валидацию")
	}

	empty := filepath.Join(dir, "empty.pbf")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validatePBF(empty); err == nil {
		t.Fatal("пустой файл не должен проходить валидацию")
	}
}

func TestEnsurePBFLocalFileValidated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local.pbf")
	if err := os.WriteFile(path, []byte("not a pbf"), 0o644); err != nil {
		t.Fatal(err)
	}

	u := testUpdater(path)
	if _, err := u.ensurePBF(t.Context()); err == nil {
		t.Fatal("невалидный локальный PBF должен приводить к ошибке")
	}
}
