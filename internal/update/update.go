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
	ImportedAt time.Time  `json:"importedAt"`
	PBF        string     `json:"pbf"`
	PBFBytes   int64      `json:"pbfBytes"`
	PBFURL     string     `json:"pbfUrl,omitempty"`
	Tiles      []TileFile `json:"tiles"`
}

// Updater выполняет полный цикл обновления данных.
type Updater struct {
	cfg config.Config
	log *slog.Logger
}

// New создаёт апдейтер.
func New(cfg config.Config, log *slog.Logger) *Updater {
	return &Updater{cfg: cfg, log: log}
}

// Run выполняет обновление: расширения → PBF → импорт → представления → тайлы → маркер.
func (u *Updater) Run(ctx context.Context) (err error) {
	started := time.Now()
	if err := u.prepareDirs(); err != nil {
		return err
	}

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

	extensions, err := schema.Extensions()
	if err != nil {
		return err
	}
	views, err := schema.Views()
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

	if err := u.importOSM(ctx, pbf.path); err != nil {
		return fmt.Errorf("импорт OSM: %w", err)
	}

	if err := db.ExecScripts(ctx, u.cfg.DatabaseDSN, views...); err != nil {
		return fmt.Errorf("представления geo: %w", err)
	}
	if _, err := pool.Exec(ctx, "ANALYZE geo.zones, geo.streets, geo.addresses, geo.names"); err != nil {
		return fmt.Errorf("analyze матвью: %w", err)
	}

	tiles, err := u.buildTiles(ctx, pbf.path)
	if err != nil {
		return fmt.Errorf("сборка тайлов: %w", err)
	}

	state := State{
		ImportedAt: time.Now().UTC(),
		PBF:        pbf.path,
		PBFBytes:   pbf.size,
		PBFURL:     u.cfg.PBFURL,
		Tiles:      tiles,
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
			u.log.Warn("не удалось снять advisory lock", "error", err)
		}
		conn.Release()
	}, nil
}

// writeState атомарно пишет маркер состояния (temp + rename).
func (u *Updater) writeState(state State) error {
	return writeJSONAtomic(u.cfg.StatePath(), state)
}

func writeJSONAtomic(path string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return nil
}
