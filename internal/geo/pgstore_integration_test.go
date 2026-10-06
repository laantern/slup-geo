//go:build integration

package geo

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laantern/slup-geo/internal/db"
	"github.com/laantern/slup-geo/internal/schema"
)

// TestPGStoreIntegration проверяет SQL-схему geo и запросы PGStore на живом PostGIS:
// расширения → фикстура planet_osm_* → представления → use case'ы.
// Запуск: TEST_DATABASE_URL=... go test -tags=integration ./internal/geo/...
func TestPGStoreIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан — интеграционный тест пропущен")
	}

	ctx := context.Background()

	extensions, err := schema.Extensions()
	if err != nil {
		t.Fatal(err)
	}
	views, err := schema.Views()
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("../../scripts/ci-fixture-planet.sql")
	if err != nil {
		t.Fatal(err)
	}

	if err := db.ExecScripts(ctx, dsn, extensions, string(fixture)); err != nil {
		t.Fatalf("подготовка БД: %v", err)
	}
	if err := db.ExecScripts(ctx, dsn, views...); err != nil {
		t.Fatalf("применение представлений: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := NewPGStore(pool)

	// Локация точки: дом-полигон в 0 м от контура + область.
	location, err := NewPointService(store).Get(ctx, 52.3905, 30.9505)
	if err != nil {
		t.Fatalf("point: %v", err)
	}
	if location.Text == nil || *location.Text != "улица Бородина, д. 2" {
		t.Fatalf("point text = %v, ожидался адрес дома", location.Text)
	}
	if len(location.Zones) == 0 || location.Zones[0].Level != LevelHouse {
		t.Fatalf("первой должна быть зона дома: %+v", location.Zones)
	}

	// Подсказки: дом по улице и области, дубликатов N/W нет.
	items, err := NewSuggestService(store).Suggest(ctx, "Бородина")
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	houses := 0
	for _, item := range items {
		if item.Level == LevelHouse {
			houses++
			if item.Name != "улица Бородина, д. 2" && item.Name != "улица Бородина, д. 4" {
				t.Fatalf("неожиданный дом в подсказках: %+v", item)
			}
		}
	}
	if houses != 2 {
		t.Fatalf("домов в подсказках = %d, ожидалось 2 (W2 и N20)", houses)
	}

	// Границы: дом-нода отдаётся Point (контракт), зона — полигоном.
	area, err := NewAreaService(store).Get(ctx, "N20", false)
	if err != nil {
		t.Fatalf("areas N20: %v", err)
	}
	if !strings.HasPrefix(string(area.Geometry), `{"type":"Point"`) {
		t.Fatalf("геометрия N20 = %s, ожидался Point", area.Geometry)
	}
	if area.Name != "улица Бородина, д. 4" {
		t.Fatalf("имя дома N20 = %q, ожидался формат как в /point и /suggest", area.Name)
	}

	zone, err := NewAreaService(store).Get(ctx, "W1", false)
	if err != nil {
		t.Fatalf("areas W1: %v", err)
	}
	if !strings.HasPrefix(string(zone.Geometry), `{"type":"Polygon"`) {
		t.Fatalf("геометрия W1 = %s, ожидался Polygon", zone.Geometry)
	}

	// Версия схемы записана представлениями.
	version, err := store.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if version != schema.Version {
		t.Fatalf("schema version = %d, ожидалась %d", version, schema.Version)
	}
}
