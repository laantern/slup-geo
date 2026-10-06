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

// buildTiles собирает векторные тайлы из локального PBF (tilemaker, схема OpenMapTiles)
// и обновляет манифест tiles.json.
func (u *Updater) buildTiles(ctx context.Context, pbfPath string) ([]TileFile, error) {
	if u.cfg.TilesEnabled {
		if err := u.extractTiles(ctx, pbfPath); err != nil {
			return nil, err
		}
	} else {
		u.log.Warn("TILES_ENABLED=false — тайлы не пересобираются, отдаём прежние")
	}

	files, err := u.scanTiles()
	if err != nil {
		return nil, err
	}
	manifest := tilesManifest{
		Files:       files,
		Attribution: osmAttribution,
		GeneratedAt: time.Now().UTC(),
	}
	if err := writeJSONAtomic(filepath.Join(u.cfg.TilesDir(), "tiles.json"), manifest); err != nil {
		return nil, fmt.Errorf("запись tiles.json: %w", err)
	}
	return files, nil
}

// extractTiles запускает tilemaker: PBF → версионный basemap-*.pmtiles (temp + rename).
// Промежуточные данные (--store) пишутся в каталог данных и удаляются после сборки.
func (u *Updater) extractTiles(ctx context.Context, pbfPath string) error {
	version := time.Now().UTC().Format("20060102T150405Z")
	name := tilesFilePrefix + version + tilesFileSuffix
	finalPath := filepath.Join(u.cfg.TilesDir(), name)

	tmpDir := u.cfg.TmpDir()
	storeDir := filepath.Join(tmpDir, "tilemaker")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return fmt.Errorf("создание временного каталога: %w", err)
	}
	defer os.RemoveAll(tmpDir) // промежуточные данные больше не нужны

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
		return err
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("публикация файла тайлов: %w", err)
	}

	// Оставляем только свежий файл: старые версии не нужны, ссылка на актуальную — в tiles.json.
	existing, err := filepath.Glob(filepath.Join(u.cfg.TilesDir(), tilesFilePrefix+"*"+tilesFileSuffix))
	if err != nil {
		return err
	}
	for _, path := range existing {
		if filepath.Base(path) != name {
			if err := os.Remove(path); err != nil {
				u.log.Warn("не удалось удалить старый файл тайлов", "файл", path, "error", err)
			}
		}
	}

	u.log.Info("сборка тайлов завершена", "файл", name)
	return nil
}

// scanTiles перечисляет актуальные файлы тайлов (новые первыми).
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
