package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactText(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{
			name: "userinfo и query в тексте ошибки",
			in:   `Get "https://user:secret@example.com/belarus.pbf?token=abcd": dial tcp`,
			want: `Get "https://***@example.com/belarus.pbf?***": dial tcp`,
		},
		{
			name: "без чувствительных частей",
			in:   "https://example.com/belarus.pbf",
			want: "https://example.com/belarus.pbf",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactText(tc.in); got != tc.want {
				t.Fatalf("redactText = %q, ожидалось %q", got, tc.want)
			}
		})
	}
}

func TestRedactURLMalformed(t *testing.T) {
	// Невалидный URL (пробел) раньше утекал целиком вместе с паролем и токеном.
	got := redactURL("https://user:SuperSecret@exa mple.com/file?token=abcd")
	if strings.Contains(got, "SuperSecret") {
		t.Fatalf("пароль не замаскирован: %q", got)
	}
	if strings.Contains(got, "token=abcd") {
		t.Fatalf("query-токен не замаскирован: %q", got)
	}
}

func TestParsePBFURLDoesNotLeak(t *testing.T) {
	_, err := parsePBFURL("https://user:SuperSecret@exa mple.com/file")
	if err == nil {
		t.Fatal("ожидалась ошибка для некорректного URL")
	}
	if strings.Contains(err.Error(), "SuperSecret") {
		t.Fatalf("ошибка содержит пароль: %v", err)
	}
}

func TestIsWritable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isWritable(path) {
		t.Fatal("обычный файл должен быть записываемым")
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	if isWritable(path) {
		t.Fatal("read-only файл не должен считаться записываемым")
	}
}
