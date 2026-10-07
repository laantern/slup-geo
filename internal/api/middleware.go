package api

import (
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
