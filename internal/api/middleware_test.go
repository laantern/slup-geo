package api

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// writerWithReadFrom — нижний приёмник, который умеет io.ReaderFrom и запоминает,
// что этот путь действительно использовали: именно так ведёт себя настоящий
// ResponseWriter при отдаче файла (sendfile).
type writerWithReadFrom struct {
	*httptest.ResponseRecorder
	readFromCalls int
}

func (w *writerWithReadFrom) ReadFrom(src io.Reader) (int64, error) {
	w.readFromCalls++
	return io.Copy(w.ResponseRecorder, src)
}

// TestStatusRecorderKeepsReadFrom — обёртка access-лога не должна отбирать
// io.ReaderFrom у нижнего уровня: иначе http.ServeFile теряет sendfile и тайлы
// копируются через userspace.
func TestStatusRecorderKeepsReadFrom(t *testing.T) {
	under := &writerWithReadFrom{ResponseRecorder: httptest.NewRecorder()}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	handler := withMiddleware(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Источник без io.WriterTo: bytes.Reader умеет WriteTo, и io.Copy ушёл бы
		// коротким путём мимо ReadFrom — так же, как CopyN с io.LimitedReader
		// в http.ServeFile (там WriteTo нет, и работает именно ReadFrom).
		src := struct{ io.Reader }{bytes.NewReader([]byte("тело тайла"))}
		if _, err := io.Copy(w, src); err != nil {
			t.Fatalf("копирование: %v", err)
		}
	}))
	handler.ServeHTTP(under, httptest.NewRequest(http.MethodGet, "/tiles/basemap.pmtiles", nil))

	if under.readFromCalls == 0 {
		t.Fatal("io.ReaderFrom нижнего уровня не вызван — sendfile потерян")
	}
	if got := under.Body.String(); got != "тело тайла" {
		t.Fatalf("тело ответа: %q", got)
	}
}

// TestStatusRecorderReadFromFallback — если нижний уровень ReadFrom не умеет,
// тело всё равно доходит целиком и без рекурсии.
func TestStatusRecorderReadFromFallback(t *testing.T) {
	under := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: under, status: http.StatusOK}

	written, err := rec.ReadFrom(bytes.NewReader([]byte("осторожно, рекурсия")))
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if written != int64(len("осторожно, рекурсия")) || under.Body.String() != "осторожно, рекурсия" {
		t.Fatalf("записано %d байт, тело: %q", written, under.Body.String())
	}
}

// TestStatusRecorderUnwrap — http.ResponseController должен добираться до
// нижнего уровня, иначе обёртка прячет Flush/Hijack от вызывающего кода.
func TestStatusRecorderUnwrap(t *testing.T) {
	under := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: under, status: http.StatusOK}

	if got := rec.Unwrap(); got != under {
		t.Fatalf("Unwrap вернул %T, ожидали исходный ResponseWriter", got)
	}
	if err := http.NewResponseController(rec).Flush(); err != nil {
		t.Fatalf("ResponseController.Flush через обёртку: %v", err)
	}
	if !under.Flushed {
		t.Fatal("Flush не дошёл до нижнего уровня")
	}
}

// TestStatusRecorderKeepsStatus — добавленные методы не ломают access-лог:
// записанный код ответа остаётся тем, что отправили.
func TestStatusRecorderKeepsStatus(t *testing.T) {
	under := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: under, status: http.StatusOK}

	rec.WriteHeader(http.StatusNotFound)
	if rec.status != http.StatusNotFound || under.Code != http.StatusNotFound {
		t.Fatalf("код записан как %d, отправлен %d", rec.status, under.Code)
	}
}
