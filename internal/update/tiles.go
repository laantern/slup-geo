package update

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// buildTiles собирает PMTiles из внешнего источника (Protomaps build) и обновляет tiles.json.
// Пустой TILES_URL — шаг пропускается, остаётся прежний набор файлов.
func (u *Updater) buildTiles(ctx context.Context) ([]TileFile, error) {
	if u.cfg.TilesURL != "" {
		if err := u.extractTiles(ctx); err != nil {
			return nil, err
		}
	} else {
		u.log.Warn("TILES_URL не задан — тайлы не пересобираются, отдаём прежние")
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

// extractTiles скачивает bbox-выборку из TILES_URL в версионный файл (temp + rename).
func (u *Updater) extractTiles(ctx context.Context) error {
	version := time.Now().UTC().Format("20060102T150405Z")
	name := tilesFilePrefix + version + tilesFileSuffix
	finalPath := filepath.Join(u.cfg.TilesDir(), name)
	tmpPath := finalPath + ".part"

	args := []string{
		"extract", u.cfg.TilesURL, tmpPath,
		"--bbox=" + u.cfg.TilesBBox,
		"--minzoom=" + fmt.Sprintf("%d", u.cfg.TilesMinZoom),
		"--maxzoom=" + fmt.Sprintf("%d", u.cfg.TilesMaxZoom),
		"--download-threads=4",
	}

	u.log.Info("сборка тайлов начата", "источник", u.cfg.TilesURL, "bbox", u.cfg.TilesBBox)
	if err := runCommand(ctx, "pmtiles", args, os.Environ()); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
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
