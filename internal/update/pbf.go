package update

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

const (
	// downloadTimeout — верхняя граница одной загрузки PBF (тело ответа может литься долго).
	downloadTimeout = 6 * time.Hour
	// maxPBFBytes — защитный лимит размера PBF: больше планеты не бывает,
	// но бесконечный/подменённый поток не должен исчерпать диск.
	maxPBFBytes = 100 << 30
)

// pbfFile — подготовленный локальный PBF.
type pbfFile struct {
	path       string
	size       int64
	downloaded bool
}

// ensurePBF возвращает локальный PBF: использует кэш при актуальности (If-Modified-Since),
// иначе скачивает по PBF_URL во временный файл и атомарно заменяет.
// Если загрузка не удалась, а локальный файл есть и валиден — работаем на нём:
// транзиентный сбой зеркала не должен отменять обновление данных.
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
		if err := validatePBF(u.cfg.PBFPath); err != nil {
			return pbfFile{}, err
		}
		u.log.Info("используется локальный PBF (PBF_URL не задан)", "файл", u.cfg.PBFPath)
		return pbfFile{path: u.cfg.PBFPath, size: stat.Size()}, nil
	}

	downloaded, err := u.downloadPBF(ctx, stat)
	if err != nil {
		if exists && validatePBF(u.cfg.PBFPath) == nil {
			u.log.Warn("загрузка PBF не удалась — используем локальный файл",
				"ошибка", err, "файл", u.cfg.PBFPath, "байт", stat.Size())
			return pbfFile{path: u.cfg.PBFPath, size: stat.Size()}, nil
		}
		return pbfFile{}, err
	}
	info, err := os.Stat(u.cfg.PBFPath)
	if err != nil {
		return pbfFile{}, fmt.Errorf("проверка скачанного PBF: %w", err)
	}
	return pbfFile{path: u.cfg.PBFPath, size: info.Size(), downloaded: downloaded}, nil
}

func (u *Updater) downloadPBF(ctx context.Context, cached os.FileInfo) (bool, error) {
	source, err := parsePBFURL(u.cfg.PBFURL)
	if err != nil {
		return false, err
	}

	// Тело ответа может литься часами: ограничиваем общую длительность загрузки,
	// чтобы «вечно висящий» сервер не держал update.
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	client := &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
			ResponseHeaderTimeout: 60 * time.Second,
			Proxy:                 http.ProxyFromEnvironment,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("слишком много редиректов")
			}
			if req.URL.Scheme != "https" {
				return fmt.Errorf("редирект на небезопасную схему %q запрещён", req.URL.Scheme)
			}
			return nil
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

		downloaded, err := u.tryDownloadPBF(ctx, client, source, cached)
		if err == nil {
			return downloaded, nil
		}
		lastErr = err
	}
	return false, fmt.Errorf("загрузка PBF: %w", lastErr)
}

func (u *Updater) tryDownloadPBF(ctx context.Context, client *http.Client, source *url.URL, cached os.FileInfo) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.String(), nil)
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
	if total > maxPBFBytes {
		return false, fmt.Errorf("PBF больше лимита %d ГБ (Content-Length %d)", maxPBFBytes>>30, total)
	}
	u.log.Info("загрузка PBF", "url", redactURL(u.cfg.PBFURL), "байт", total)

	tmpPath := u.cfg.PBFPath + ".part"
	_ = os.Remove(tmpPath)
	file, err := os.Create(tmpPath)
	if err != nil {
		return false, err
	}
	defer func() {
		file.Close()
		os.Remove(tmpPath)
	}()

	written, err := copyWithProgress(ctx, file, io.LimitReader(response.Body, maxPBFBytes+1), total, u)
	if err != nil {
		return false, err
	}
	if written > maxPBFBytes {
		return false, fmt.Errorf("размер PBF превысил лимит %d ГБ", maxPBFBytes>>30)
	}
	if total > 0 && written != total {
		return false, fmt.Errorf("загрузка неполная: получено %d из %d байт", written, total)
	}
	if err := file.Sync(); err != nil {
		return false, err
	}
	if err := file.Close(); err != nil {
		return false, err
	}
	if err := validatePBF(tmpPath); err != nil {
		return false, err
	}
	if err := os.Rename(tmpPath, u.cfg.PBFPath); err != nil {
		return false, fmt.Errorf("публикация PBF: %w", err)
	}
	u.log.Info("PBF загружен", "байт", written)
	return true, nil
}

// parsePBFURL проверяет источник: только https (http — для локальных зеркал).
func parsePBFURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("разбор PBF_URL: %w", err)
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(parsed.Hostname()) {
			return nil, fmt.Errorf("PBF_URL по http разрешён только для локальных зеркал (localhost/127.0.0.1), используйте https: %s", redactURL(raw))
		}
	default:
		return nil, fmt.Errorf("PBF_URL должен начинаться с https:// (получена схема %q)", parsed.Scheme)
	}
	return parsed, nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// validatePBF проверяет, что файл начинается с корректного OSM PBF-заголовка:
// это отсекает HTML-страницы ошибок, обрезанные и подменённые файлы до импорта.
func validatePBF(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("открытие PBF %s: %w", path, err)
	}
	defer file.Close()

	header := make([]byte, 4)
	if _, err := io.ReadFull(file, header); err != nil {
		return fmt.Errorf("PBF %s повреждён (нет заголовка): %w", path, err)
	}
	size := binary.BigEndian.Uint32(header)
	if size == 0 || size > 64*1024 {
		return fmt.Errorf("PBF %s повреждён: некорректный размер заголовка блока", path)
	}
	blob := make([]byte, size)
	if _, err := io.ReadFull(file, blob); err != nil {
		return fmt.Errorf("PBF %s повреждён (обрезанный заголовок): %w", path, err)
	}
	if !bytes.Contains(blob, []byte("OSMHeader")) {
		return fmt.Errorf("PBF %s не похож на OSM PBF: в заголовке нет OSMHeader", path)
	}
	return nil
}

// redactURL убирает из URL учётные данные перед логированием и записью в состояние.
func redactURL(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw
	}
	parsed.User = nil
	return parsed.String()
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
