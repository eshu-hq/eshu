package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

type fakeStatusReader struct {
	snapshot statuspkg.RawSnapshot
	err      error
}

func (f fakeStatusReader) ReadStatusSnapshot(_ context.Context, _ time.Time) (statuspkg.RawSnapshot, error) {
	if f.err != nil {
		return statuspkg.RawSnapshot{}, f.err
	}
	return f.snapshot, nil
}

func TestStatusHandlerLegacyIndexStatusAlias(t *testing.T) {
	t.Parallel()

	handler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC),
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/index-status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := payload["status"], "healthy"; got != want {
		t.Fatalf("payload[status] = %#v, want %#v", got, want)
	}
}

func TestStatusHandlerLegacyIngesterAliases(t *testing.T) {
	t.Parallel()

	handler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC),
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	listReq := httptest.NewRequest(http.MethodGet, "/api/v0/ingesters", nil)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)

	if got, want := listRec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /api/v0/ingesters status = %d, want %d", got, want)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/v0/ingesters/repository", nil)
	detailRec := httptest.NewRecorder()
	mux.ServeHTTP(detailRec, detailReq)

	if got, want := detailRec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /api/v0/ingesters/repository status = %d, want %d", got, want)
	}

	var detailPayload map[string]any
	if err := json.Unmarshal(detailRec.Body.Bytes(), &detailPayload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := detailPayload["ingester"], "repository"; got != want {
		t.Fatalf("payload[ingester] = %#v, want %#v", got, want)
	}
}

func TestStatusHandlerCollectorsRouteExposesCoordinatorInstances(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	handler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: now,
				Coordinator: &statuspkg.CoordinatorSnapshot{
					CollectorInstances: []statuspkg.CollectorInstanceSummary{{
						InstanceID:     "collector-git-default",
						CollectorKind:  "git",
						Mode:           "continuous",
						Enabled:        true,
						Bootstrap:      true,
						ClaimsEnabled:  false,
						LastObservedAt: now,
						UpdatedAt:      now,
					}},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/collectors", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /api/v0/status/collectors status = %d, want %d", got, want)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := int(payload["count"].(float64)), 1; got != want {
		t.Fatalf("payload[count] = %d, want %d", got, want)
	}
}
