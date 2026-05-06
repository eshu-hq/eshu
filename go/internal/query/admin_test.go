package query

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// --- stub implementations ---

type stubRecoveryHandler struct {
	refinalizeResult recovery.RefinalizeResult
	refinalizeErr    error
	replayResult     recovery.ReplayResult
	replayErr        error
}

type stubReindexRequester struct {
	ingesters []string
	err       error
}

func (s *stubRecoveryHandler) Refinalize(_ context.Context, _ recovery.RefinalizeFilter) (recovery.RefinalizeResult, error) {
	return s.refinalizeResult, s.refinalizeErr
}

func (s *stubRecoveryHandler) ReplayFailed(_ context.Context, _ recovery.ReplayFilter) (recovery.ReplayResult, error) {
	return s.replayResult, s.replayErr
}

func (s *stubReindexRequester) RequestReindex(_ context.Context, ingester string) error {
	s.ingesters = append(s.ingesters, ingester)
	return s.err
}

type stubAdminStore struct {
	workItems     []AdminWorkItem
	workItemsErr  error
	deadLettered  []AdminWorkItem
	deadLetterErr error
	skipped       []AdminWorkItem
	skipErr       error
	replayed      []AdminWorkItem
	replayErr     error
	backfillRow   *AdminBackfillRequest
	backfillErr   error
	replayEvents  []AdminReplayEvent
	replayEvtErr  error
	decisions     []AdminDecisionRow
	decisionsErr  error
	evidence      []AdminEvidenceRow
	evidenceErr   error
}

func (s *stubAdminStore) ListWorkItems(_ context.Context, _ WorkItemFilter) ([]AdminWorkItem, error) {
	return s.workItems, s.workItemsErr
}

func (s *stubAdminStore) DeadLetterWorkItems(_ context.Context, _ DeadLetterFilter) ([]AdminWorkItem, error) {
	return s.deadLettered, s.deadLetterErr
}

func (s *stubAdminStore) SkipRepositoryWorkItems(_ context.Context, _ string, _ string) ([]AdminWorkItem, error) {
	return s.skipped, s.skipErr
}

func (s *stubAdminStore) ReplayFailedWorkItems(_ context.Context, _ ReplayWorkItemFilter) ([]AdminWorkItem, error) {
	return s.replayed, s.replayErr
}

func (s *stubAdminStore) RequestBackfill(_ context.Context, _ BackfillInput) (*AdminBackfillRequest, error) {
	return s.backfillRow, s.backfillErr
}

func (s *stubAdminStore) ListReplayEvents(_ context.Context, _ ReplayEventFilter) ([]AdminReplayEvent, error) {
	return s.replayEvents, s.replayEvtErr
}

func (s *stubAdminStore) ListDecisions(_ context.Context, _ DecisionQueryFilter) ([]AdminDecisionRow, error) {
	return s.decisions, s.decisionsErr
}

func (s *stubAdminStore) ListEvidence(_ context.Context, _ string) ([]AdminEvidenceRow, error) {
	return s.evidence, s.evidenceErr
}

// --- helpers ---

func newAdminMux(h *AdminHandler) *http.ServeMux {
	mux := http.NewServeMux()
	h.Mount(mux)
	return mux
}

func postJSON(mux *http.ServeMux, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func getJSON(mux *http.ServeMux, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v\nbody: %s", err, w.Body.String())
	}
	return got
}

// strPtr returns a pointer to s for use in test fixtures.
func strPtr(s string) *string {
	return &s
}

// Ensure sql.DB is referenced so the import is valid in future test
// extensions that require database integration.
var _ *sql.DB

// --- core admin endpoint tests ---

func TestAdminHandler_Refinalize(t *testing.T) {
	stub := &stubRecoveryHandler{
		refinalizeResult: recovery.RefinalizeResult{
			Enqueued: 2,
			ScopeIDs: []string{"scope-1", "scope-2"},
		},
	}
	h := &AdminHandler{Recovery: stub}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/refinalize", map[string]any{
		"scope_ids": []string{"scope-1", "scope-2"},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	got := decodeBody(t, w)
	if int(got["enqueued"].(float64)) != 2 {
		t.Errorf("enqueued = %v, want 2", got["enqueued"])
	}
}

func TestAdminHandler_Refinalize_MissingBody(t *testing.T) {
	stub := &stubRecoveryHandler{}
	h := &AdminHandler{Recovery: stub}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/refinalize", map[string]any{})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAdminHandler_RefinalizeStatusRouteRemovedFromPublicMount(t *testing.T) {
	h := &AdminHandler{Recovery: &stubRecoveryHandler{}}
	mux := newAdminMux(h)

	w := getJSON(mux, "/api/v0/admin/refinalize/status")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestAdminHandler_TuningReport(t *testing.T) {
	h := &AdminHandler{}
	mux := newAdminMux(h)

	w := getJSON(mux, "/api/v0/admin/shared-projection/tuning-report")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	got := decodeBody(t, w)
	if got["status"] != "not_applicable" {
		t.Errorf("status = %q, want %q", got["status"], "not_applicable")
	}
}

func TestAdminHandler_Reindex(t *testing.T) {
	requester := &stubReindexRequester{}
	h := &AdminHandler{Reindexer: requester}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/reindex", map[string]any{
		"ingester": "repository",
		"scope":    "workspace",
		"force":    true,
	})

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusAccepted, w.Body.String())
	}

	got := decodeBody(t, w)
	if got["status"] != "accepted" {
		t.Errorf("status = %q, want %q", got["status"], "accepted")
	}
	if got, want := len(requester.ingesters), 1; got != want {
		t.Fatalf("RequestReindex call count = %d, want %d", got, want)
	}
	if got, want := requester.ingesters[0], "repository"; got != want {
		t.Fatalf("RequestReindex ingester = %q, want %q", got, want)
	}
}

func TestAdminHandler_Mount_RegistersAllRoutes(t *testing.T) {
	stub := &stubRecoveryHandler{}
	store := &stubAdminStore{}
	h := &AdminHandler{Recovery: stub, Store: store}
	mux := newAdminMux(h)

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v0/admin/refinalize"},
		{http.MethodGet, "/api/v0/admin/shared-projection/tuning-report"},
		{http.MethodPost, "/api/v0/admin/reindex"},
		{http.MethodPost, "/api/v0/admin/work-items/query"},
		{http.MethodPost, "/api/v0/admin/decisions/query"},
		{http.MethodPost, "/api/v0/admin/dead-letter"},
		{http.MethodPost, "/api/v0/admin/skip"},
		{http.MethodPost, "/api/v0/admin/replay"},
		{http.MethodPost, "/api/v0/admin/backfill"},
		{http.MethodPost, "/api/v0/admin/replay-events/query"},
	}

	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			var body *bytes.Buffer
			if rt.method == http.MethodPost {
				body = bytes.NewBufferString("{}")
			} else {
				body = &bytes.Buffer{}
			}
			req := httptest.NewRequest(rt.method, rt.path, body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			// Any route should not return 404/405 -- it is registered
			if w.Code == http.StatusNotFound || w.Code == http.StatusMethodNotAllowed {
				t.Errorf("route %s %s returned %d, expected it to be registered", rt.method, rt.path, w.Code)
			}
		})
	}
}
