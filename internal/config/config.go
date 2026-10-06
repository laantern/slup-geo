// Package config читает параметры апплаенса из переменных окружения.
// Единственный источник конфигурации — ENV (см. README): БД, креды и инструменты
// не администрируются снаружи.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Config — полный набор настроек процесса.
type Config struct {
	// HTTPAddr — адрес внутреннего API.
	HTTPAddr string

	// DatabaseDSN — строка подключения к PostgreSQL (встроенной в контейнер при штатном запуске).
	DatabaseDSN string

	// DataDir — корень данных (одним volume): БД, PBF, тайлы, состояние.
	DataDir string
	// PBFURL — откуда скачивать OSM PBF (пусто — использовать только локальный файл).
	PBFURL string
	// PBFPath — локальный файл PBF (кэш загрузки/офлайн-импорт).
	PBFPath string

	// UpdateOnStart — обновлять данные при старте контейнера даже при непустой БД.
	UpdateOnStart bool
	// UpdateSchedule — cron-расписание обновлений внутри процесса (пусто — выключено).
	UpdateSchedule string

	// TilesEnabled — собирать векторные тайлы из PBF (tilemaker).
	TilesEnabled bool
	// TilemakerConfig — путь к конфигу слоёв (OpenMapTiles-профиль).
	TilemakerConfig string
	// TilemakerProcess — путь к Lua-профилю обработки тегов.
	TilemakerProcess string
	// TilemakerThreads — число потоков tilemaker (0 — авто).
	TilemakerThreads int

	// WebDir — каталог веб-ассетов карты: стиль, спрайт, глифы.
	WebDir string
	// ExampleEnabled — отдавать страницу-пример /example (удобно в dev; в проде можно выключить).
	ExampleEnabled bool

	// ImportProcesses — число процессов osm2pgsql.
	ImportProcesses int

	// ShutdownTimeout — время на аккуратную остановку HTTP-сервера.
	ShutdownTimeout time.Duration
}

// TilesDir возвращает каталог файлов тайлов.
func (c Config) TilesDir() string { return filepath.Join(c.DataDir, "tiles") }

// TmpDir возвращает каталог временных файлов (например, промежуточные данные tilemaker).
func (c Config) TmpDir() string { return filepath.Join(c.DataDir, "tmp") }

// StateDir возвращает каталог состояния (маркер импорта, tiles.json и т.п.).
func (c Config) StateDir() string { return filepath.Join(c.DataDir, "state") }

// StatePath возвращает путь файла состояния.
func (c Config) StatePath() string { return filepath.Join(c.StateDir(), "state.json") }

// StatusPath возвращает путь файла статуса последнего обновления.
func (c Config) StatusPath() string { return filepath.Join(c.StateDir(), "status.json") }

// Load собирает конфигурацию из ENV, подставляя штатные значения по умолчанию.
func Load() (Config, error) {
	dataDir := env("DATA_DIR", "/data")

	threads, err := envInt("TILEMAKER_THREADS", 0, 0, 256)
	if err != nil {
		return Config{}, err
	}
	importProcesses, err := envInt("IMPORT_PROCESSES", runtime.NumCPU(), 1, 256)
	if err != nil {
		return Config{}, err
	}
	shutdownSeconds, err := envInt("SHUTDOWN_TIMEOUT_SECONDS", 15, 1, 3600)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		HTTPAddr: env("HTTP_ADDR", ":8080"),

		DataDir: dataDir,
		// PBF_URL намеренно без дефолта: офлайн-режим (только PBF_PATH) и «что качать» задаёт окружение.
		PBFURL:  envRaw("PBF_URL"),
		PBFPath: env("PBF_PATH", filepath.Join(dataDir, "osm", "belarus-latest.osm.pbf")),

		UpdateSchedule: env("UPDATE_SCHEDULE", ""),

		TilemakerConfig:  env("TILEMAKER_CONFIG", "/usr/local/share/tilemaker/config-openmaptiles.json"),
		TilemakerProcess: env("TILEMAKER_PROCESS", "/usr/local/share/tilemaker/process-openmaptiles.lua"),
		TilemakerThreads: threads,

		WebDir: env("WEB_DIR", "/usr/local/share/slup-geo"),

		ImportProcesses: importProcesses,
		ShutdownTimeout: time.Duration(shutdownSeconds) * time.Second,
	}

	var err2 error
	if cfg.UpdateOnStart, err2 = envBool("UPDATE_ON_START", false); err2 != nil {
		return Config{}, err2
	}
	if cfg.TilesEnabled, err2 = envBool("TILES_ENABLED", true); err2 != nil {
		return Config{}, err2
	}
	if cfg.ExampleEnabled, err2 = envBool("ENABLE_EXAMPLE", true); err2 != nil {
		return Config{}, err2
	}

	cfg.DatabaseDSN, err = databaseDSN()
	if err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// databaseDSN собирает строку подключения: DATABASE_URL имеет приоритет,
// иначе стандартные PG* переменные. Пароль обязателен: дефолтного пароля нет,
// в штатном запуске его генерирует entrypoint.
func databaseDSN() (string, error) {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return dsn, nil
	}

	host := env("PGHOST", "127.0.0.1")
	port := env("PGPORT", "5432")
	user := env("PGUSER", "geo_user")
	password := os.Getenv("PGPASSWORD")
	database := env("PGDATABASE", "geo_db")
	if strings.TrimSpace(password) == "" {
		return "", fmt.Errorf("PGPASSWORD не задан: при штатном запуске пароль создаёт entrypoint, при внешней БД задайте PGPASSWORD или DATABASE_URL")
	}

	return fmt.Sprintf(
		"postgres://%s:%s@%s/%s?sslmode=disable",
		url.QueryEscape(user), url.QueryEscape(password), joinHostPort(host, port), url.PathEscape(database),
	), nil
}

func joinHostPort(host, port string) string {
	if strings.HasPrefix(host, "/") {
		// Unix-сокет: postgres://user:pass@/db?host=/var/run/postgresql
		return ""
	}
	return host + ":" + port
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// envRaw возвращает значение без подстановки дефолта (пусто = не задано).
func envRaw(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

// envInt читает целочисленную переменную со строгой проверкой диапазона:
// мусор или значение вне диапазона — ошибка конфигурации, а не тихий дефолт.
func envInt(key string, fallback, min, max int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("переменная %s должна быть целым числом, получено %q", key, raw)
	}
	if v < min || v > max {
		return 0, fmt.Errorf("переменная %s должна быть в диапазоне %d..%d, получено %d", key, min, max, v)
	}
	return v, nil
}

func envBool(key string, fallback bool) (bool, error) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch raw {
	case "":
		return fallback, nil
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("переменная %s должна быть true/false, получено %q", key, raw)
	}
}
