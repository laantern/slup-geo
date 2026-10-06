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
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ошибка конфигурации:", err)
		os.Exit(2)
	}

	log := newLogger()

	switch command() {
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

	pool, err := db.Connect(ctx, cfg.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("подключение к БД: %w", err)
	}
	defer pool.Close()

	// Расширения нужны для запросов (pg_trgm) — идемпотентно обеспечиваем их на старте.
	if extensions, err := schema.Extensions(); err == nil {
		if err := db.ExecScripts(ctx, cfg.DatabaseDSN, extensions); err != nil {
			log.Warn("не удалось применить расширения БД", "error", err)
		}
	}

	var initialized bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('geo.zones') IS NOT NULL").Scan(&initialized); err != nil {
		log.Warn("не удалось проверить схему geo", "error", err)
	} else if !initialized {
		log.Warn("схема geo не инициализирована — выполните slup-geo update или дождитесь первого импорта")
	}

	store := geo.NewPGStore(pool)
	handlers := &api.Handlers{
		Points:  geo.NewPointService(store),
		Suggest: geo.NewSuggestService(store),
		Areas:   geo.NewAreaService(store),
		Health:  pool,
		Tiles: &api.TilesHandler{
			Dir:    cfg.TilesDir(),
			WebDir: cfg.WebDir,
			Log:    log,
		},
		ExampleEnabled: cfg.ExampleEnabled,
		Log:            log,
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.NewRouter(handlers),
		ReadHeaderTimeout: 10 * time.Second,
	}

	scheduler, err := update.New(cfg, log).StartSchedule()
	if err != nil {
		return err
	}

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
