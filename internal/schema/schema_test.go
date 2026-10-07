package schema

import (
	"strings"
	"testing"
)

// stripComments убирает SQL-комментарии: в них упоминаются оба варианта имён,
// и проверки должны смотреть только на исполняемые инструкции.
func stripComments(script string) string {
	var builder strings.Builder
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		builder.WriteString(line)
		builder.WriteString("\n")
	}
	return builder.String()
}

// TestViewsRendering проверяет подстановку плейсхолдеров: канонический вариант — без суффиксов,
// staging — по planet_osm_next_* и с матвью/индексами geo.*_next.
func TestViewsRendering(t *testing.T) {
	canonical, err := Views()
	if err != nil {
		t.Fatal(err)
	}
	staging, err := ViewsStaging()
	if err != nil {
		t.Fatal(err)
	}

	canonicalJoined := stripComments(strings.Join(canonical, "\n"))
	stagingJoined := stripComments(strings.Join(staging, "\n"))

	for name, script := range map[string]string{"канонический": canonicalJoined, "staging": stagingJoined} {
		if strings.Contains(script, "{{") {
			t.Fatalf("%s вариант: остались неподставленные плейсхолдеры", name)
		}
		if !strings.Contains(script, "CREATE TABLE IF NOT EXISTS geo.meta") {
			t.Fatalf("%s вариант: geo.meta должен быть в обоих вариантах", name)
		}
	}

	for _, forbidden := range []string{"planet_osm_next_", "geo.zones_next", "geo.names_next", "zones_next_geom_idx"} {
		if strings.Contains(canonicalJoined, forbidden) {
			t.Fatalf("канонический вариант не должен содержать %q", forbidden)
		}
	}
	for _, want := range []string{"geo.zones ", "zones_geom_idx", "FROM planet_osm_polygon"} {
		if !strings.Contains(canonicalJoined, want) {
			t.Fatalf("канонический вариант не содержит %q", want)
		}
	}

	for _, want := range []string{
		"planet_osm_next_polygon", "planet_osm_next_line", "planet_osm_next_point",
		"geo.zones_next", "geo.streets_next", "geo.addresses_next", "geo.names_next",
		"zones_next_geom_idx", "names_next_entity_uidx",
	} {
		if !strings.Contains(stagingJoined, want) {
			t.Fatalf("staging-вариант не содержит %q", want)
		}
	}
	if strings.Contains(stagingJoined, "_next_next") {
		t.Fatal("staging-вариант: двойная подстановка суффикса")
	}
}
