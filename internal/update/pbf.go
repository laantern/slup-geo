package update

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

// pbfFile — подготовленный локальный PBF.
type pbfFile struct {
	path       string
	size       int64
	downloaded bool
}

// ensurePBF возвращает локальный PBF: использует кэш при актуальности (If-Modified-Since),
// иначе скачивает по PBF_URL во временный файл и атомарно заменяет.
func (u *Updater) ensurePBF(ctx context.Context) (pbfFile, error) {
	stat, statErr := os.Stat(u.cfg.PBFPath)
	exists := statErr == nil
	if exists && !stat.Mode().IsRegular() {
		return pbfFile{}, fmt.Errorf("PBF_PATH (%s) существует, но это не файл (bind-mount каталога?)", u.cfg.PBFPath)
	}

	if !exists && u.cfg.PBFURL == "" {
		return pbfFile{}, fmt.Errorf("PBF_URL не задан, а локальный файл %s не найден", u.cfg.PBFPath)
	}
	if exists && u.cfg.PBFURL == "" {
		u.log.Info("используется локальный PBF (PBF_URL не задан)", "файл", u.cfg.PBFPath)
		return pbfFile{path: u.cfg.PBFPath, size: stat.Size()}, nil
	}

	downloaded, err := u.downloadPBF(ctx, stat)
	if err != nil {
		return pbfFile{}, err
	}
	info, err := os.Stat(u.cfg.PBFPath)
	if err != nil {
		return pbfFile{}, fmt.Errorf("проверка скачанного PBF: %w", err)
	}
	return pbfFile{path: u.cfg.PBFPath, size: info.Size(), downloaded: downloaded}, nil
}

func (u *Updater) downloadPBF(ctx context.Context, cached os.FileInfo) (bool, error) {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
			ResponseHeaderTimeout: 60 * time.Second,
			Proxy:                 http.ProxyFromEnvironment,
		},
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if attempt > 1 {
			u.log.Warn("повтор загрузки PBF", "попытка", attempt, "ошибка", lastErr)
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-time.After(5 * time.Second):
			}
		}

		downloaded, err := u.tryDownloadPBF(ctx, client, cached)
		if err == nil {
			return downloaded, nil
		}
		lastErr = err
	}
	return false, fmt.Errorf("загрузка PBF: %w", lastErr)
}

func (u *Updater) tryDownloadPBF(ctx context.Context, client *http.Client, cached os.FileInfo) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.cfg.PBFURL, nil)
	if err != nil {
		return false, err
	}
	if cached != nil {
		// Проверяем актуальность: Geofabrik отдаёт 304 на If-Modified-Since.
		request.Header.Set("If-Modified-Since", cached.ModTime().UTC().Format(http.TimeFormat))
	}

	response, err := client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusNotModified:
		u.log.Info("PBF актуален, загрузка не требуется", "файл", u.cfg.PBFPath)
		return false, nil
	case http.StatusOK:
	default:
		return false, fmt.Errorf("неожиданный статус %s", response.Status)
	}

	total := response.ContentLength
	u.log.Info("загрузка PBF", "url", u.cfg.PBFURL, "байт", total)

	tmpPath := u.cfg.PBFPath + ".part"
	file, err := os.Create(tmpPath)
	if err != nil {
		return false, err
	}
	defer func() {
		file.Close()
		os.Remove(tmpPath)
	}()

	written, err := copyWithProgress(ctx, file, response.Body, total, u)
	if err != nil {
		return false, err
	}
	if err := file.Sync(); err != nil {
		return false, err
	}
	if err := file.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tmpPath, u.cfg.PBFPath); err != nil {
		return false, err
	}
	u.log.Info("PBF загружен", "байт", written)
	return true, nil
}

func copyWithProgress(ctx context.Context, dst io.Writer, src io.Reader, total int64, u *Updater) (int64, error) {
	buffer := make([]byte, 1<<20)
	var written int64
	lastLogged := int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		n, readErr := src.Read(buffer)
		if n > 0 {
			written += int64(n)
			if _, writeErr := dst.Write(buffer[:n]); writeErr != nil {
				return written, writeErr
			}
			if total > 0 {
				progress := written * 100 / total
				if progress >= lastLogged+20 {
					lastLogged = progress - progress%20
					u.log.Info("прогресс загрузки PBF", "процент", lastLogged)
				}
			}
		}
		if readErr == io.EOF {
			return written, nil
		}
		if readErr != nil {
			return written, readErr
		}
	}
}
