package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// ingesterSelectionSnapshot returns a snapshot whose health, queue, and
// coordinator sections are populated without any fact_records-derived section,
// so the ingester handler tests can prove the rendered fields survive a filtered
// snapshot that skips collector fact evidence and registry collectors.
func ingesterSelectionSnapshot(asOf time.Time) statuspkg.RawSnapshot {
	return statuspkg.RawSnapshot{
		AsOf: asOf,
		Queue: statuspkg.QueueSnapshot{
			Total:       9,
			Outstanding: 4,
			Pending:     3,
			InFlight:    1,
		},
		Coordinator: &statuspkg.CoordinatorSnapshot{
			CollectorInstances: []statuspkg.CollectorInstanceSummary{
				{
					InstanceID:     "collector-a",
					CollectorKind:  "git",
					Mode:           "continuous",
					Enabled:        true,
					LastObservedAt: asOf,
					UpdatedAt:      asOf,
				},
				{
					InstanceID:     "collector-b",
					CollectorKind:  "git",
					Mode:           "continuous",
					Enabled:        true,
					LastObservedAt: asOf,
					UpdatedAt:      asOf,
				},
			},
			ActiveClaims: 2,
		},
	}
}

func TestListIngestersRequestsFilteredSelection(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	reader := &selectionRecordingReader{snapshot: ingesterSelectionSnapshot(asOf)}
	handler := &StatusHandler{StatusReader: reader}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/ingesters", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if reader.filteredCallCount != 1 {
		t.Fatalf("filtered reader call count = %d, want 1", reader.filteredCallCount)
	}
	if reader.lastSelection.IncludeCollectorFactEvidence {
		t.Fatalf("list ingesters requested collector fact evidence; want excluded")
	}
	if reader.lastSelection.IncludeRegistryCollectors {
		t.Fatalf("list ingesters requested registry collectors; want excluded")
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	ingesters, ok := payload["ingesters"].([]any)
	if !ok || len(ingesters) != 1 {
		t.Fatalf("payload.ingesters = %#v, want one entry", payload["ingesters"])
	}
	first, ok := ingesters[0].(map[string]any)
	if !ok {
		t.Fatalf("payload.ingesters[0] = %#v, want object", ingesters[0])
	}
	if got, want := first["queue_outstanding"], float64(4); got != want {
		t.Fatalf("queue_outstanding = %#v, want %#v", got, want)
	}
	if got, want := first["collector_instances"], float64(2); got != want {
		t.Fatalf("collector_instances = %#v, want %#v", got, want)
	}
	if _, ok := first["health"]; !ok {
		t.Fatalf("ingester entry missing health field: %#v", first)
	}
}

func TestGetIngesterStatusRequestsFilteredSelection(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	reader := &selectionRecordingReader{snapshot: ingesterSelectionSnapshot(asOf)}
	handler := &StatusHandler{StatusReader: reader}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/ingesters/repository", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if reader.filteredCallCount != 1 {
		t.Fatalf("filtered reader call count = %d, want 1", reader.filteredCallCount)
	}
	if reader.lastSelection.IncludeCollectorFactEvidence {
		t.Fatalf("ingester status requested collector fact evidence; want excluded")
	}
	if reader.lastSelection.IncludeRegistryCollectors {
		t.Fatalf("ingester status requested registry collectors; want excluded")
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	for _, key := range []string{"health", "queue", "coordinator", "stage_summaries", "domain_backlogs"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("ingester status payload missing %q field: %#v", key, payload)
		}
	}
	queue, ok := payload["queue"].(map[string]any)
	if !ok {
		t.Fatalf("payload.queue = %#v, want object", payload["queue"])
	}
	if got, want := queue["outstanding"], float64(4); got != want {
		t.Fatalf("queue.outstanding = %#v, want %#v", got, want)
	}
	coordinator, ok := payload["coordinator"].(map[string]any)
	if !ok {
		t.Fatalf("payload.coordinator = %#v, want object", payload["coordinator"])
	}
	instances, ok := coordinator["collector_instances"].([]any)
	if !ok || len(instances) != 2 {
		t.Fatalf("coordinator.collector_instances = %#v, want two entries", coordinator["collector_instances"])
	}
}
