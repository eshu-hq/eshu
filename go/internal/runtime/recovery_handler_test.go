package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

func TestNewRecoveryHandlerRequiresHandler(t *testing.T) {
	t.Parallel()

	_, err := NewRecoveryHandler(nil)
	if err == nil {
		t.Fatal("NewRecoveryHandler(nil) error = nil, want non-nil")
	}
}

func TestRecoveryHandlerReplayReturnsReplayedItems(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStore{
		replayResult: recovery.ReplayResult{
			Stage:       recovery.StageProjector,
			Replayed:    2,
			WorkItemIDs: []string{"item-1", "item-2"},
		},
	}
	handler := mustNewRecoveryHandler(t, store)

	body := mustMarshal(t, replayRequest{
		Stage:    "projector",
		ScopeIDs: []string{"s1"},
		Limit:    10,
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/replay", bytes.NewReader(body))

	handler.handleReplay(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("POST /admin/replay status = %d, want %d", got, want)
	}

	var resp replayResponse
	mustUnmarshal(t, recorder.Body.Bytes(), &resp)
	if resp.Status != "replayed" {
		t.Fatalf("response status = %q, want %q", resp.Status, "replayed")
	}
	if resp.Replayed != 2 {
		t.Fatalf("response replayed = %d, want 2", resp.Replayed)
	}
	if len(resp.WorkItemIDs) != 2 {
		t.Fatalf("response work_item_ids len = %d, want 2", len(resp.WorkItemIDs))
	}
}

func TestRecoveryHandlerReplayRejectsGetMethod(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStore{}
	handler := mustNewRecoveryHandler(t, store)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/replay", nil)

	handler.handleReplay(recorder, request)

	if got, want := recorder.Code, http.StatusMethodNotAllowed; got != want {
		t.Fatalf("GET /admin/replay status = %d, want %d", got, want)
	}
}

func TestRecoveryHandlerReplayRejectsInvalidStage(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStore{}
	handler := mustNewRecoveryHandler(t, store)

	body := mustMarshal(t, replayRequest{Stage: "invalid"})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/replay", bytes.NewReader(body))

	handler.handleReplay(recorder, request)

	if got, want := recorder.Code, http.StatusUnprocessableEntity; got != want {
		t.Fatalf("POST /admin/replay invalid stage status = %d, want %d", got, want)
	}
}

func TestRecoveryHandlerReplayPropagatesStoreError(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStore{replayErr: errors.New("database down")}
	handler := mustNewRecoveryHandler(t, store)

	body := mustMarshal(t, replayRequest{Stage: "projector"})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/replay", bytes.NewReader(body))

	handler.handleReplay(recorder, request)

	if got, want := recorder.Code, http.StatusUnprocessableEntity; got != want {
		t.Fatalf("POST /admin/replay store error status = %d, want %d", got, want)
	}
}

func TestRecoveryHandlerRefinalizeReturnsEnqueuedScopes(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStore{
		refinalizeResult: recovery.RefinalizeResult{
			Enqueued: 2,
			ScopeIDs: []string{"s1", "s2"},
		},
	}
	handler := mustNewRecoveryHandler(t, store)

	body := mustMarshal(t, refinalizeRequest{ScopeIDs: []string{"s1", "s2"}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/refinalize", bytes.NewReader(body))

	handler.handleRefinalize(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("POST /admin/refinalize status = %d, want %d", got, want)
	}

	var resp refinalizeResponse
	mustUnmarshal(t, recorder.Body.Bytes(), &resp)
	if resp.Status != "enqueued" {
		t.Fatalf("response status = %q, want %q", resp.Status, "enqueued")
	}
	if resp.Enqueued != 2 {
		t.Fatalf("response enqueued = %d, want 2", resp.Enqueued)
	}
}

func TestRecoveryHandlerRefinalizeRejectsEmptyScopeIDs(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStore{}
	handler := mustNewRecoveryHandler(t, store)

	body := mustMarshal(t, refinalizeRequest{ScopeIDs: []string{}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/refinalize", bytes.NewReader(body))

	handler.handleRefinalize(recorder, request)

	if got, want := recorder.Code, http.StatusUnprocessableEntity; got != want {
		t.Fatalf("POST /admin/refinalize empty scopes status = %d, want %d", got, want)
	}
}

func TestRecoveryHandlerRefinalizeRejectsGetMethod(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStore{}
	handler := mustNewRecoveryHandler(t, store)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/refinalize", nil)

	handler.handleRefinalize(recorder, request)

	if got, want := recorder.Code, http.StatusMethodNotAllowed; got != want {
		t.Fatalf("GET /admin/refinalize status = %d, want %d", got, want)
	}
}

func TestRecoveryHandlerMount(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStore{
		replayResult: recovery.ReplayResult{Stage: recovery.StageProjector},
	}
	handler := mustNewRecoveryHandler(t, store)

	mux := http.NewServeMux()
	handler.Mount(mux)

	// Verify replay route is mounted
	body := mustMarshal(t, replayRequest{Stage: "projector"})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/replay", bytes.NewReader(body))
	mux.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("mounted POST /admin/replay status = %d, want %d", got, want)
	}
}

// --- helpers ---

func mustNewRecoveryHandler(t *testing.T, store recovery.ReplayStore) *RecoveryHandler {
	t.Helper()

	recoveryHandler, err := recovery.NewHandler(store)
	if err != nil {
		t.Fatalf("recovery.NewHandler() error = %v", err)
	}

	httpHandler, err := NewRecoveryHandler(recoveryHandler)
	if err != nil {
		t.Fatalf("NewRecoveryHandler() error = %v", err)
	}

	return httpHandler
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	return data
}

func mustUnmarshal(t *testing.T, data []byte, v any) {
	t.Helper()

	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, body = %s", err, string(data))
	}
}

// --- fakes ---

type fakeRecoveryStore struct {
	replayResult     recovery.ReplayResult
	replayErr        error
	refinalizeResult recovery.RefinalizeResult
	refinalizeErr    error
}

func (f *fakeRecoveryStore) ReplayFailedWorkItems(
	_ context.Context,
	_ recovery.ReplayFilter,
	_ time.Time,
) (recovery.ReplayResult, error) {
	return f.replayResult, f.replayErr
}

func (f *fakeRecoveryStore) RefinalizeScopeProjections(
	_ context.Context,
	_ recovery.RefinalizeFilter,
	_ time.Time,
) (recovery.RefinalizeResult, error) {
	return f.refinalizeResult, f.refinalizeErr
}
