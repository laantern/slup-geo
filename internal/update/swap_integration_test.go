//go:build integration

package update

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laantern/slup-geo/internal/config"
	"github.com/laantern/slup-geo/internal/db"
	"github.com/laantern/slup-geo/internal/schema"
)

// TestSwapIntegration проверяет двухфазную замену данных на живом PostGIS:
// старая генерация planet_osm_* + канонические матвью → staging-генерация planet_osm_next_*
// → сборка geo.*_next вне транзакции → короткий swap → канонические имена и данные staging.
// Отдельно проверяется сбой swap: рабочие данные обязаны остаться нетронутыми.
// Запуск: TEST_DATABASE_URL=... go test -tags=integration ./internal/update/...
func TestSwapIntegration(t *testing.T) {
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

	// Старая генерация: planet_osm_* и канонические матвью.
	if err := db.ExecScripts(ctx, dsn, extensions, string(fixture)); err != nil {
		t.Fatalf("подготовка старой генерации: %v", err)
	}
	if err := db.ExecScripts(ctx, dsn, views...); err != nil {
		t.Fatalf("канонические представления: %v", err)
	}

	// staging-генерация: те же таблицы с суффиксом _next и другой областью.
	staging := func(label string) string {
		sql := strings.ReplaceAll(string(fixture), "planet_osm_", "planet_osm_next_")
		return strings.ReplaceAll(sql, "Гомельская область", label)
	}

	u := New(config.Config{DatabaseDSN: dsn}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := db.ExecScripts(ctx, dsn, staging("Минская область")); err != nil {
		t.Fatalf("staging-импорт: %v", err)
	}
	if err := u.buildViewsStaging(ctx); err != nil {
		t.Fatalf("staging-представления: %v", err)
	}
	if err := u.swapPlanetTables(ctx); err != nil {
		t.Fatalf("swap: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	assertCount(t, pool, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relname LIKE 'planet_osm_next%'`, 0,
		"staging-таблицы должны исчезнуть после swap")
	assertCount(t, pool, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'geo' AND c.relname LIKE '%\_next' ESCAPE '\'`, 0,
		"staging-матвью и их индексы должны исчезнуть после swap")
	assertCount(t, pool, `SELECT count(*) FROM pg_indexes
		WHERE schemaname = 'geo' AND tablename = 'zones' AND indexname = 'zones_geom_idx'`, 1,
		"канонический индекс zones_geom_idx должен существовать")
	assertCount(t, pool, `SELECT count(*) FROM pg_indexes
		WHERE schemaname = 'geo' AND indexname LIKE '%\_next\_%' ESCAPE '\'`, 0,
		"индексов со staging-именами быть не должно")

	var label string
	if err := pool.QueryRow(ctx, `SELECT label FROM geo.zones LIMIT 1`).Scan(&label); err != nil {
		t.Fatal(err)
	}
	if label != "Минская область" {
		t.Fatalf("geo.zones.label = %q, ожидалась «Минская область» (данные staging)", label)
	}

	// Сбойная замена: без planet_osm_next_roads swap обязан откатиться,
	// а рабочие данные — остаться прежними.
	if err := db.ExecScripts(ctx, dsn, staging("Брестская область")); err != nil {
		t.Fatalf("staging-импорт 2: %v", err)
	}
	if err := u.buildViewsStaging(ctx); err != nil {
		t.Fatalf("staging-представления 2: %v", err)
	}
	if err := db.ExecScripts(ctx, dsn, "DROP TABLE planet_osm_next_roads CASCADE"); err != nil {
		t.Fatalf("подготовка сбоя: %v", err)
	}
	if err := u.swapPlanetTables(ctx); err == nil {
		t.Fatal("swap должен был упасть без planet_osm_next_roads")
	}
	if err := pool.QueryRow(ctx, `SELECT label FROM geo.zones LIMIT 1`).Scan(&label); err != nil {
		t.Fatal(err)
	}
	if label != "Минская область" {
		t.Fatalf("после сбойного swap данные = %q, ожидались прежние («Минская область»)", label)
	}
}

func assertCount(t *testing.T, pool *pgxpool.Pool, query string, want int, msg string) {
	t.Helper()
	var got int
	if err := pool.QueryRow(context.Background(), query).Scan(&got); err != nil {
		t.Fatalf("%s: %v", msg, err)
	}
	if got != want {
		t.Fatalf("%s: получено %d, ожидалось %d", msg, got, want)
	}
}
