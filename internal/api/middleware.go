package api

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// exampleCSP — политика для страницы-пример: только свои ресурсы; воркер MapLibre
// поднимается с того же origin (worker-src 'self') либо из blob.
const exampleCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; connect-src 'self'; worker-src 'self' blob:; child-src 'self' blob:; " +
	"base-uri 'none'; form-action 'none'"

// withMiddleware добавляет security-заголовки и access-лог (без query — в строке
// запроса бывают адреса, их в логи не пишем).
func withMiddleware(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/example") {
			w.Header().Set("Content-Security-Policy", exampleCSP)
		}

		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		// Healthcheck и тайлы запрашиваются постоянно — им достаточно debug-уровня.
		level := slog.LevelInfo
		if strings.HasPrefix(r.URL.Path, "/health") || strings.HasPrefix(r.URL.Path, "/tiles/") || r.URL.Path == "/status" {
			level = slog.LevelDebug
		}
		log.Log(r.Context(), level, "http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}

// noCache заставляет клиента ревалидировать файлы (по Last-Modified), чтобы
// обновления страницы-примера подхватывались сразу после апгрейда образа.
func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

// statusRecorder запоминает код ответа для access-лога.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Unwrap отдаёт нижний ResponseWriter для http.ResponseController: без него
// обёртка прячет возможности транспорта (Flush, Hijack, дедлайны) от тех, кто их
// ищет type-assertion'ом. Реализовывать Flush здесь самому нельзя: обёртка тогда
// обещала бы Flusher и там, где нижний уровень его не умеет.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// ReadFrom сохраняет sendfile: http.ServeFile копирует тело через io.Copy, а тот
// предпочитает io.ReaderFrom у получателя. Без этого метода крупные файлы тайлов
// (/tiles/*) копировались бы через userspace вместо sendfile.
func (r *statusRecorder) ReadFrom(src io.Reader) (int64, error) {
	if rf, ok := r.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(src)
	}
	// Обёртка над writer'ом гасит ReadFrom у самой обёртки — иначе io.Copy вернулся
	// бы сюда же и ушёл в рекурсию.
	return io.Copy(struct{ io.Writer }{r.ResponseWriter}, src)
}
