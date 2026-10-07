package update

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// tilesManifest — содержимое /tiles/tiles.json: какой файл карты сейчас актуален.
type tilesManifest struct {
	Files       []TileFile `json:"files"`
	Attribution string     `json:"attribution"`
	GeneratedAt time.Time  `json:"generatedAt"`
}

const (
	tilesFilePrefix = "basemap-"
	tilesFileSuffix = ".pmtiles"
	osmAttribution  = "© OpenStreetMap contributors"
)

// builtTiles — собранный, но ещё не опубликованный файл тайлов.
type builtTiles struct {
	tmpPath string
	name    string
}

// buildTiles собирает векторные тайлы из локального PBF (tilemaker, схема OpenMapTiles)
// во временный файл. Публикация (rename в каталог тайлов + манифест) происходит после
// успешной замены таблиц БД — см. publishTiles.
func (u *Updater) buildTiles(ctx context.Context, pbfPath string) (*builtTiles, error) {
	if !u.cfg.TilesEnabled {
		u.log.Warn("TILES_ENABLED=false — тайлы не пересобираются, отдаём прежние")
		return nil, nil
	}

	version := time.Now().UTC().Format("20060102T150405Z")
	name := tilesFilePrefix + version + tilesFileSuffix

	tmpDir := u.cfg.TmpDir()
	storeDir := filepath.Join(tmpDir, "tilemaker")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return nil, fmt.Errorf("создание временного каталога: %w", err)
	}

	tmpPath := filepath.Join(tmpDir, name)
	args := []string{
		"--input", pbfPath,
		"--output", tmpPath,
		"--config", u.cfg.TilemakerConfig,
		"--process", u.cfg.TilemakerProcess,
		"--threads", strconv.Itoa(u.cfg.TilemakerThreads),
		"--store", storeDir,
	}

	u.log.Info("сборка тайлов tilemaker начата", "pbf", pbfPath, "потоков", u.cfg.TilemakerThreads)
	if err := runCommand(ctx, "tilemaker", args, os.Environ()); err != nil {
		return nil, err
	}
	u.log.Info("сборка тайлов завершена (ожидает публикации)", "файл", name)
	return &builtTiles{tmpPath: tmpPath, name: name}, nil
}

// publishTiles публикует собранный файл (если есть) и обновляет манифест.
// Порядок важен: сначала rename и манифест, только потом удаление старых версий —
// иначе манифест может сослаться на уже удалённый файл.
func (u *Updater) publishTiles(built *builtTiles) ([]TileFile, error) {
	var current *TileFile

	if built != nil {
		finalPath := filepath.Join(u.cfg.TilesDir(), built.name)
		if err := os.Rename(built.tmpPath, finalPath); err != nil {
			return nil, fmt.Errorf("публикация файла тайлов: %w", err)
		}
		info, err := os.Stat(finalPath)
		if err != nil {
			return nil, fmt.Errorf("проверка опубликованного файла тайлов: %w", err)
		}
		current = &TileFile{Name: built.name, SizeBytes: info.Size(), BuiltAt: info.ModTime().UTC()}
	} else {
		existing, err := u.scanTiles()
		if err != nil {
			return nil, err
		}
		if len(existing) > 0 {
			current = &existing[0]
		}
	}

	files := make([]TileFile, 0, 1)
	if current != nil {
		files = append(files, *current)
	}
	manifest := tilesManifest{
		Files:       files,
		Attribution: osmAttribution,
		GeneratedAt: time.Now().UTC(),
	}
	if err := writeJSONAtomic(filepath.Join(u.cfg.TilesDir(), "tiles.json"), manifest, 0o644); err != nil {
		return nil, fmt.Errorf("запись tiles.json: %w", err)
	}

	existing, err := filepath.Glob(filepath.Join(u.cfg.TilesDir(), tilesFilePrefix+"*"+tilesFileSuffix))
	if err != nil {
		return nil, err
	}
	for _, path := range existing {
		if current != nil && filepath.Base(path) == current.Name {
			continue
		}
		if err := os.Remove(path); err != nil {
			u.log.Warn("не удалось удалить старый файл тайлов", "файл", path, "error", err)
		}
	}
	return files, nil
}

// scanTiles перечисляет файлы тайлов (новые первыми).
func (u *Updater) scanTiles() ([]TileFile, error) {
	paths, err := filepath.Glob(filepath.Join(u.cfg.TilesDir(), tilesFilePrefix+"*"+tilesFileSuffix))
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))

	files := make([]TileFile, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !strings.HasSuffix(path, tilesFileSuffix) {
			continue
		}
		files = append(files, TileFile{
			Name:      filepath.Base(path),
			SizeBytes: info.Size(),
			BuiltAt:   info.ModTime().UTC(),
		})
	}
	return files, nil
}
