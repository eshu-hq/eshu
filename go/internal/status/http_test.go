package status_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestNewHTTPHandlerRequiresReader(t *testing.T) {
	t.Parallel()

	_, err := status.NewHTTPHandler(nil, status.HTTPHandlerOptions{})
	if err == nil {
		t.Fatal("NewHTTPHandler() error = nil, want non-nil")
	}
}

func TestHTTPHandlerRendersTextByDefault(t *testing.T) {
	t.Parallel()

	handler := mustNewStatusHandler(t, &fakeReader{
		snapshot: status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			Queue: status.QueueSnapshot{
				Outstanding:          2,
				InFlight:             1,
				OldestOutstandingAge: 45 * time.Second,
			},
		},
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/status", nil)

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("ServeHTTP() status = %d, want %d", got, want)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.Contains(got, "text/plain") {
		t.Fatalf("ServeHTTP() Content-Type = %q, want text/plain", got)
	}
	if got := recorder.Body.String(); !strings.Contains(got, "Health: progressing") {
		t.Fatalf("ServeHTTP() body = %q, want text status report", got)
	}
	if got := recorder.Body.String(); !strings.Contains(got, "Version: "+buildinfo.AppVersion()) {
		t.Fatalf("ServeHTTP() body = %q, want version line", got)
	}
}

func TestHTTPHandlerRendersJSONWhenRequested(t *testing.T) {
	t.Parallel()

	handler := mustNewStatusHandler(t, &fakeReader{
		snapshot: status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
		},
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/status?format=json", nil)

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("ServeHTTP() status = %d, want %d", got, want)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("ServeHTTP() Content-Type = %q, want application/json", got)
	}
	if got := recorder.Body.String(); !strings.Contains(got, "\"health\"") {
		t.Fatalf("ServeHTTP() body = %q, want json report", got)
	}
	if got := recorder.Body.String(); !strings.Contains(got, "\"version\": \""+buildinfo.AppVersion()+"\"") {
		t.Fatalf("ServeHTTP() body = %q, want version field", got)
	}
}

func TestHTTPHandlerUsesAcceptHeaderForJSON(t *testing.T) {
	t.Parallel()

	handler := mustNewStatusHandler(t, &fakeReader{
		snapshot: status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
		},
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/status", nil)
	request.Header.Set("Accept", "application/json")

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("ServeHTTP() status = %d, want %d", got, want)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("ServeHTTP() Content-Type = %q, want application/json", got)
	}
}

func TestHTTPHandlerRejectsUnsupportedFormat(t *testing.T) {
	t.Parallel()

	handler := mustNewStatusHandler(t, &fakeReader{})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/status?format=yaml", nil)

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusBadRequest; got != want {
		t.Fatalf("ServeHTTP() status = %d, want %d", got, want)
	}
	if got := recorder.Body.String(); !strings.Contains(got, "unsupported format") {
		t.Fatalf("ServeHTTP() body = %q, want unsupported format message", got)
	}
}

func TestHTTPHandlerRejectsUnsupportedMethod(t *testing.T) {
	t.Parallel()

	handler := mustNewStatusHandler(t, &fakeReader{})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/status", nil)

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusMethodNotAllowed; got != want {
		t.Fatalf("ServeHTTP() status = %d, want %d", got, want)
	}
	if got, want := recorder.Header().Get("Allow"), "GET, HEAD"; got != want {
		t.Fatalf("ServeHTTP() Allow = %q, want %q", got, want)
	}
}

func TestHTTPHandlerSupportsHeadRequests(t *testing.T) {
	t.Parallel()

	handler := mustNewStatusHandler(t, &fakeReader{
		snapshot: status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
		},
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodHead, "/admin/status?format=json", nil)

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("ServeHTTP() status = %d, want %d", got, want)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("ServeHTTP() Content-Type = %q, want application/json", got)
	}
	if got := recorder.Body.String(); got != "" {
		t.Fatalf("ServeHTTP() body = %q, want empty", got)
	}
}

func TestHTTPHandlerPropagatesReaderErrors(t *testing.T) {
	t.Parallel()

	handler := mustNewStatusHandler(t, &fakeReader{err: errors.New("boom")})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/status", nil)

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("ServeHTTP() status = %d, want %d", got, want)
	}
	if got := recorder.Body.String(); !strings.Contains(got, "load status report") {
		t.Fatalf("ServeHTTP() body = %q, want load report message", got)
	}
}

func mustNewStatusHandler(t *testing.T, reader status.Reader) http.Handler {
	t.Helper()

	handler, err := status.NewHTTPHandler(
		reader,
		status.HTTPHandlerOptions{
			Now: func() time.Time {
				return time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC)
			},
		},
	)
	if err != nil {
		t.Fatalf("NewHTTPHandler() error = %v, want nil", err)
	}

	return handler
}
