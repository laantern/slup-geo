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

	// TilesURL — источник PMTiles (Protomaps build), пусто — тайлы не собираются.
	TilesURL string
	// TilesBBox — bbox выборки тайлов: minLon,minLat,maxLon,maxLat.
	TilesBBox string
	// TilesMinZoom/TilesMaxZoom — диапазон зумов выборки тайлов.
	TilesMinZoom int
	TilesMaxZoom int

	// ImportProcesses — число процессов osm2pgsql.
	ImportProcesses int

	// ShutdownTimeout — время на аккуратную остановку HTTP-сервера.
	ShutdownTimeout time.Duration
}

// TilesDir возвращает каталог файлов тайлов.
func (c Config) TilesDir() string { return filepath.Join(c.DataDir, "tiles") }

// StateDir возвращает каталог состояния (маркер импорта, tiles.json и т.п.).
func (c Config) StateDir() string { return filepath.Join(c.DataDir, "state") }

// StatePath возвращает путь файла состояния.
func (c Config) StatePath() string { return filepath.Join(c.StateDir(), "state.json") }

// Load собирает конфигурацию из ENV, подставляя штатные значения по умолчанию.
func Load() (Config, error) {
	dataDir := env("DATA_DIR", "/data")

	cfg := Config{
		HTTPAddr: env("HTTP_ADDR", ":8080"),

		DataDir: dataDir,
		// PBF_URL намеренно без дефолта: офлайн-режим (только PBF_PATH) и «что качать» задаёт окружение.
		PBFURL:  envRaw("PBF_URL"),
		PBFPath: env("PBF_PATH", filepath.Join(dataDir, "osm", "belarus-latest.osm.pbf")),

		UpdateSchedule: env("UPDATE_SCHEDULE", ""),
		TilesURL:       env("TILES_URL", ""),
		TilesBBox:      env("TILES_BBOX", "23.1,51.2,32.8,56.2"),
		TilesMinZoom:   envInt("TILES_MINZOOM", 0),
		TilesMaxZoom:   envInt("TILES_MAXZOOM", 15),

		ImportProcesses: envInt("IMPORT_PROCESSES", runtime.NumCPU()),
		ShutdownTimeout: time.Duration(envInt("SHUTDOWN_TIMEOUT_SECONDS", 15)) * time.Second,
	}

	var err error
	if cfg.UpdateOnStart, err = envBool("UPDATE_ON_START", false); err != nil {
		return Config{}, err
	}

	cfg.DatabaseDSN, err = databaseDSN()
	if err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// databaseDSN собирает строку подключения: DATABASE_URL имеет приоритет,
// иначе стандартные PG* переменные.
func databaseDSN() (string, error) {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return dsn, nil
	}

	host := env("PGHOST", "127.0.0.1")
	port := env("PGPORT", "5432")
	user := env("PGUSER", "geo_user")
	password := env("PGPASSWORD", "geo_password")
	database := env("PGDATABASE", "geo_db")

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

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
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
