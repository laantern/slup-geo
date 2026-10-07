// slup-geo — гео-апплаенс Slup: внутренний API (point/suggest/areas) и тайлы.
//
// Команды:
//
//	slup-geo serve   — HTTP-сервис (внутренний API + /tiles/*), при UPDATE_SCHEDULE — cron обновлений
//	slup-geo update  — полный цикл обновления данных: PBF → импорт → схема → тайлы → маркер
//	slup-geo version — версия сборки
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/laantern/slup-geo/internal/api"
	"github.com/laantern/slup-geo/internal/config"
	"github.com/laantern/slup-geo/internal/db"
	"github.com/laantern/slup-geo/internal/geo"
	"github.com/laantern/slup-geo/internal/schema"
	"github.com/laantern/slup-geo/internal/update"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd := command()
	applyPasswordFallback(cmd)

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ошибка конфигурации:", err)
		os.Exit(2)
	}

	log := newLogger()

	switch cmd {
	case "serve":
		if err := serve(cfg, log); err != nil {
			log.Error("serve завершился с ошибкой", "error", err)
			os.Exit(1)
		}
	case "update":
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		if err := update.New(cfg, log).Run(ctx); err != nil {
			log.Error("update завершился с ошибкой", "error", err)
			os.Exit(1)
		}
	case "version":
		fmt.Printf("slup-geo %s (commit %s, %s)\n", version, commit, date)
	default:
		fmt.Fprintln(os.Stderr, "использование: slup-geo <serve|update|version>")
		os.Exit(2)
	}
}

// applyPasswordFallback позволяет запускать slup-geo через docker exec без явных кредов:
// entrypoint сохраняет пароли ролей в volume (state/db_password_owner, db_password_reader).
// Пароль администратора (geo_user) приложению не выдаётся — см. SECURITY.md.
func applyPasswordFallback(cmd string) {
	if os.Getenv("DATABASE_URL") != "" || strings.TrimSpace(os.Getenv("PGPASSWORD")) != "" {
		return
	}

	var user, file string
	switch cmd {
	case "update":
		user, file = "geo_owner", "db_password_owner"
	case "serve":
		user, file = "geo_reader", "db_password_reader"
	default:
		return
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "/data"
	}
	data, err := os.ReadFile(filepath.Join(dataDir, "state", file))
	if err != nil {
		return
	}
	password := strings.TrimSpace(string(data))
	if password == "" {
		return
	}
	_ = os.Setenv("PGPASSWORD", password)
	if current := os.Getenv("PGUSER"); current == "" || current == "geo_user" {
		_ = os.Setenv("PGUSER", user)
	}
}

func command() string {
	if len(os.Args) < 2 {
		return ""
	}
	return os.Args[1]
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	switch os.Getenv("LOG_LEVEL") {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

func serve(cfg config.Config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.ConnectServe(ctx, cfg.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("подключение к БД: %w", err)
	}
	defer pool.Close()

	// Расширения нужны для запросов (pg_trgm) — идемпотентно обеспечиваем их на старте.
	if extensions, err := schema.Extensions(); err == nil {
		if err := db.ExecScripts(ctx, cfg.DatabaseDSN, extensions); err != nil {
			log.Error("не удалось применить расширения БД (postgis, hstore, pg_trgm) — при внешней БД создайте их заранее", "error", err)
		}
	}

	// После жёсткого останова мог остаться статус running без живого процесса обновления.
	update.ClearStaleStatus(ctx, pool, cfg.StatusPath(), log)

	store := geo.NewPGStore(pool)
	initialized, err := store.SchemaReady(ctx)
	if err != nil {
		log.Warn("не удалось проверить схему geo", "error", err)
	} else if !initialized {
		log.Warn("схема geo не инициализирована — выполните slup-geo update или дождитесь первого импорта")
	} else if dataVersion, err := store.SchemaVersion(ctx); err == nil && dataVersion != schema.Version {
		log.Warn("версия схемы данных не совпадает с версией сервиса — выполните slup-geo update",
			"данные", dataVersion, "сервис", schema.Version)
	}

	handlers := &api.Handlers{
		Points:  geo.NewPointService(store),
		Suggest: geo.NewSuggestService(store),
		Areas:   geo.NewAreaService(store),
		Health:  store,
		Tiles: &api.TilesHandler{
			Dir:    cfg.TilesDir(),
			WebDir: cfg.WebDir,
			Log:    log,
		},
		Meta: api.Meta{
			StatePath:  cfg.StatePath(),
			StatusPath: cfg.StatusPath(),
			Version:    version,
		},
		ExampleEnabled: cfg.ExampleEnabled,
		Log:            log,
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.NewRouter(handlers),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      5 * time.Minute, // тайлы отдаются крупными кусками
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}

	scheduler := update.New(cfg, log).StartSchedule(ctx)

	errCh := make(chan error, 1)
	go func() {
		log.Info("HTTP-сервис запущен", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Info("остановка HTTP-сервиса")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("остановка сервера: %w", err)
	}
	if scheduler != nil {
		<-scheduler.Stop().Done()
	}
	return nil
}
