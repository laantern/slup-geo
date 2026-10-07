// Package update — команда update: скачивание PBF, импорт osm2pgsql, сборка схемы,
// тайлов и маркера состояния. Обновления выполняются под advisory lock.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laantern/slup-geo/internal/config"
	"github.com/laantern/slup-geo/internal/db"
	"github.com/laantern/slup-geo/internal/schema"
)

// advisoryLockKey — произвольный ключ блокировки обновления в PostgreSQL.
const advisoryLockKey int64 = 780_310_001

// TileFile — собранный файл тайлов.
type TileFile struct {
	Name      string    `json:"name"`
	SizeBytes int64     `json:"sizeBytes"`
	BuiltAt   time.Time `json:"builtAt"`
}

// State — состояние данных сервиса (маркер завершённого обновления).
type State struct {
	ImportedAt    time.Time  `json:"importedAt"`
	PBF           string     `json:"pbf"`
	PBFBytes      int64      `json:"pbfBytes"`
	PBFURL        string     `json:"pbfUrl,omitempty"`
	Tiles         []TileFile `json:"tiles"`
	SchemaVersion int        `json:"schemaVersion"`
}

// Status — состояние последней попытки обновления (для /health и /status).
type Status struct {
	State      string     `json:"state"` // running | ok | error
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Error      string     `json:"error,omitempty"`
}

// statusErrorLimit — сколько символов ошибки хранить в status.json: полный хвост вывода
// внешних утилит остаётся в логах, а /health и /status не должны раздуваться.
const statusErrorLimit = 500

// Updater выполняет полный цикл обновления данных.
type Updater struct {
	cfg config.Config
	log *slog.Logger
}

// New создаёт апдейтер.
func New(cfg config.Config, log *slog.Logger) *Updater {
	return &Updater{cfg: cfg, log: log}
}

// Run выполняет обновление: расширения → PBF → тайлы → staging-импорт → замена таблиц
// и представлений → публикация тайлов → маркер. Рабочие данные не трогаются до успешной
// замены: сбой на любом шаге оставляет прежние таблицы и матвью на месте.
func (u *Updater) Run(ctx context.Context) (err error) {
	started := time.Now()
	if err := u.prepareDirs(); err != nil {
		return err
	}
	defer os.RemoveAll(u.cfg.TmpDir()) // промежуточные данные tilemaker

	pool, err := db.Connect(ctx, u.cfg.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("подключение к БД: %w", err)
	}
	defer pool.Close()

	unlock, err := u.lock(ctx, pool)
	if err != nil {
		return err
	}
	defer unlock()

	status := Status{State: "running", StartedAt: time.Now().UTC()}
	if err := u.writeStatus(status); err != nil {
		u.log.Warn("не удалось записать статус обновления", "error", err)
	}
	defer func() {
		finished := time.Now().UTC()
		status.FinishedAt = &finished
		if err != nil {
			status.State = "error"
			status.Error = truncateRunes(err.Error(), statusErrorLimit)
		} else {
			status.State = "ok"
		}
		if werr := u.writeStatus(status); werr != nil {
			u.log.Warn("не удалось записать статус обновления", "error", werr)
		}
	}()

	extensions, err := schema.Extensions()
	if err != nil {
		return err
	}

	u.log.Info("обновление начато", "pbf", u.cfg.PBFPath, "тайлы", u.cfg.TilesEnabled)

	if err := db.ExecScripts(ctx, u.cfg.DatabaseDSN, extensions); err != nil {
		return fmt.Errorf("расширения БД: %w", err)
	}

	pbf, err := u.ensurePBF(ctx)
	if err != nil {
		return fmt.Errorf("подготовка PBF: %w", err)
	}
	u.log.Info("PBF готов", "файл", pbf.path, "байт", pbf.size, "скачан", pbf.downloaded)

	// Тайлы собираются до замены таблиц: сбой tilemaker не должен оставить
	// новые данные БД со старыми тайлами.
	built, err := u.buildTiles(ctx, pbf.path)
	if err != nil {
		return fmt.Errorf("сборка тайлов: %w", err)
	}

	if err := u.importOSM(ctx, pbf.path); err != nil {
		return fmt.Errorf("импорт OSM: %w", err)
	}
	if err := u.buildViewsStaging(ctx); err != nil {
		return err
	}
	if err := u.swapPlanetTables(ctx); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, "ANALYZE geo.zones, geo.streets, geo.addresses, geo.names"); err != nil {
		return fmt.Errorf("analyze матвью: %w", err)
	}

	tiles, err := u.publishTiles(built)
	if err != nil {
		return fmt.Errorf("публикация тайлов: %w", err)
	}

	state := State{
		ImportedAt:    time.Now().UTC(),
		PBF:           pbf.path,
		PBFBytes:      pbf.size,
		PBFURL:        redactURL(u.cfg.PBFURL),
		Tiles:         tiles,
		SchemaVersion: schema.Version,
	}
	if err := u.writeState(state); err != nil {
		return fmt.Errorf("запись состояния: %w", err)
	}

	u.log.Info("обновление завершено",
		"за", time.Since(started).Round(time.Second).String(),
		"тайлов", len(tiles),
	)
	return nil
}

// prepareDirs создаёт каталоги данных.
func (u *Updater) prepareDirs() error {
	for _, dir := range []string{filepath.Dir(u.cfg.PBFPath), u.cfg.TilesDir(), u.cfg.StateDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("создание каталога %s: %w", dir, err)
		}
	}
	return nil
}

// lock берёт advisory lock на отдельном соединении; повторный запуск не стартует.
func (u *Updater) lock(ctx context.Context, pool *pgxpool.Pool) (func(), error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("соединение для блокировки: %w", err)
	}

	var acquired bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", advisoryLockKey).Scan(&acquired); err != nil {
		conn.Release()
		return nil, fmt.Errorf("advisory lock: %w", err)
	}
	if !acquired {
		conn.Release()
		return nil, errors.New("обновление уже выполняется (advisory lock занят)")
	}

	return func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", advisoryLockKey); err != nil {
			// Соединение с удержанным локом нельзя возвращать в пул — закрываем его.
			u.log.Warn("не удалось снять advisory lock — соединение закрывается", "error", err)
			if raw := conn.Hijack(); raw != nil {
				_ = raw.Close(context.WithoutCancel(ctx))
			}
			return
		}
		conn.Release()
	}, nil
}

// writeState атомарно пишет маркер состояния (temp + rename).
func (u *Updater) writeState(state State) error {
	return writeJSONAtomic(u.cfg.StatePath(), state, 0o600)
}

// writeStatus атомарно пишет статус последней попытки обновления.
func (u *Updater) writeStatus(status Status) error {
	return writeJSONAtomic(u.cfg.StatusPath(), status, 0o600)
}

// ClearStaleStatus помечает незавершённое обновление как прерванное, если advisory lock свободен.
// Нужен после жёсткого останова (SIGKILL, kill контейнера): без этого /health может вечно
// показывать «starting», а /status — «running», хотя никакого обновления не идёт.
// Проверка по advisory lock, а не по pid: она одинаково работает и внутри контейнера, и на хосте.
func ClearStaleStatus(ctx context.Context, pool *pgxpool.Pool, statusPath string, log *slog.Logger) {
	payload, err := os.ReadFile(statusPath)
	if err != nil {
		return
	}
	var status Status
	if err := json.Unmarshal(payload, &status); err != nil || status.State != "running" {
		return
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		log.Warn("не удалось проверить незавершённое обновление", "error", err)
		return
	}
	defer conn.Release()

	var acquired bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", advisoryLockKey).Scan(&acquired); err != nil {
		log.Warn("не удалось проверить advisory lock обновления", "error", err)
		return
	}
	if !acquired {
		return // обновление действительно выполняется
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", advisoryLockKey); err != nil {
		// Соединение с удержанным локом нельзя возвращать в пул — закрываем его.
		log.Warn("не удалось снять advisory lock — соединение закрывается", "error", err)
		if raw := conn.Hijack(); raw != nil {
			_ = raw.Close(context.WithoutCancel(ctx))
		}
		return
	}

	finished := time.Now().UTC()
	status.State = "error"
	status.Error = "обновление прервано (процесс не завершился)"
	status.FinishedAt = &finished
	if err := writeJSONAtomic(statusPath, status, 0o600); err != nil {
		log.Warn("не удалось записать статус обновления", "error", err)
		return
	}
	log.Warn("найден незавершённый статус обновления — помечен как прерванный", "начато", status.StartedAt)
}

// writeJSONAtomic пишет JSON атомарно (temp + fsync + rename + fsync каталога).
func writeJSONAtomic(path string, value any, perm os.FileMode) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := file.Write(payload); err != nil {
		file.Close()
		os.Remove(tmp)
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

// truncateRunes обрезает строку по числу символов (не разрывая UTF-8).
func truncateRunes(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit]) + "…"
}
