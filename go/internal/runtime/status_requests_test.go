package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStatusRequestHandlerRequestScanDelegatesToStore(t *testing.T) {
	t.Parallel()

	store := &recordingStatusRequestStore{}
	handler, err := NewStatusRequestHandler(store)
	if err != nil {
		t.Fatalf("NewStatusRequestHandler() error = %v, want nil", err)
	}

	if err := handler.RequestScan(context.Background(), "ingester-1"); err != nil {
		t.Fatalf("RequestScan() error = %v, want nil", err)
	}
	if got, want := len(store.scanRequests), 1; got != want {
		t.Fatalf("scan request count = %d, want %d", got, want)
	}
	if got, want := store.scanRequests[0], "ingester-1"; got != want {
		t.Fatalf("scan request ingester = %q, want %q", got, want)
	}
}

func TestStatusRequestHandlerRequestScanRejectsEmptyIngester(t *testing.T) {
	t.Parallel()

	store := &recordingStatusRequestStore{}
	handler, err := NewStatusRequestHandler(store)
	if err != nil {
		t.Fatalf("NewStatusRequestHandler() error = %v, want nil", err)
	}

	if err := handler.RequestScan(context.Background(), ""); err == nil {
		t.Fatal("RequestScan('') error = nil, want non-nil")
	}
}

func TestStatusRequestHandlerClaimScanDelegatesToStore(t *testing.T) {
	t.Parallel()

	store := &recordingStatusRequestStore{
		claimScanResult: ScanRequest{
			Ingester: "ingester-1",
			State:    RequestStateRunning,
		},
	}
	handler, err := NewStatusRequestHandler(store)
	if err != nil {
		t.Fatalf("NewStatusRequestHandler() error = %v, want nil", err)
	}

	result, err := handler.ClaimScan(context.Background(), "ingester-1")
	if err != nil {
		t.Fatalf("ClaimScan() error = %v, want nil", err)
	}
	if got, want := result.State, RequestStateRunning; got != want {
		t.Fatalf("ClaimScan().State = %q, want %q", got, want)
	}
}

func TestStatusRequestHandlerCompleteScanDelegatesToStore(t *testing.T) {
	t.Parallel()

	store := &recordingStatusRequestStore{}
	handler, err := NewStatusRequestHandler(store)
	if err != nil {
		t.Fatalf("NewStatusRequestHandler() error = %v, want nil", err)
	}

	if err := handler.CompleteScan(context.Background(), "ingester-1", ""); err != nil {
		t.Fatalf("CompleteScan() error = %v, want nil", err)
	}
	if got, want := len(store.completeScanCalls), 1; got != want {
		t.Fatalf("complete scan call count = %d, want %d", got, want)
	}
}

func TestStatusRequestHandlerRequestReindexDelegatesToStore(t *testing.T) {
	t.Parallel()

	store := &recordingStatusRequestStore{}
	handler, err := NewStatusRequestHandler(store)
	if err != nil {
		t.Fatalf("NewStatusRequestHandler() error = %v, want nil", err)
	}

	if err := handler.RequestReindex(context.Background(), "ingester-1"); err != nil {
		t.Fatalf("RequestReindex() error = %v, want nil", err)
	}
	if got, want := len(store.reindexRequests), 1; got != want {
		t.Fatalf("reindex request count = %d, want %d", got, want)
	}
}

func TestStatusRequestHandlerClaimReindexDelegatesToStore(t *testing.T) {
	t.Parallel()

	store := &recordingStatusRequestStore{
		claimReindexResult: ReindexRequest{
			Ingester: "ingester-1",
			State:    RequestStateRunning,
		},
	}
	handler, err := NewStatusRequestHandler(store)
	if err != nil {
		t.Fatalf("NewStatusRequestHandler() error = %v, want nil", err)
	}

	result, err := handler.ClaimReindex(context.Background(), "ingester-1")
	if err != nil {
		t.Fatalf("ClaimReindex() error = %v, want nil", err)
	}
	if got, want := result.State, RequestStateRunning; got != want {
		t.Fatalf("ClaimReindex().State = %q, want %q", got, want)
	}
}

func TestStatusRequestHandlerCompleteReindexDelegatesToStore(t *testing.T) {
	t.Parallel()

	store := &recordingStatusRequestStore{}
	handler, err := NewStatusRequestHandler(store)
	if err != nil {
		t.Fatalf("NewStatusRequestHandler() error = %v, want nil", err)
	}

	if err := handler.CompleteReindex(context.Background(), "ingester-1", "some error"); err != nil {
		t.Fatalf("CompleteReindex() error = %v, want nil", err)
	}
	if got, want := len(store.completeReindexCalls), 1; got != want {
		t.Fatalf("complete reindex call count = %d, want %d", got, want)
	}
	if got, want := store.completeReindexCalls[0].reindexErr, "some error"; got != want {
		t.Fatalf("complete reindex error = %q, want %q", got, want)
	}
}

func TestStatusRequestHandlerRequiresStore(t *testing.T) {
	t.Parallel()

	_, err := NewStatusRequestHandler(nil)
	if err == nil {
		t.Fatal("NewStatusRequestHandler(nil) error = nil, want non-nil")
	}
}

func TestStatusRequestHandlerPropagatesStoreErrors(t *testing.T) {
	t.Parallel()

	store := &recordingStatusRequestStore{
		scanErr: errors.New("db unavailable"),
	}
	handler, err := NewStatusRequestHandler(store)
	if err != nil {
		t.Fatalf("NewStatusRequestHandler() error = %v, want nil", err)
	}

	if err := handler.RequestScan(context.Background(), "ingester-1"); err == nil {
		t.Fatal("RequestScan() error = nil, want non-nil")
	}
}

func TestRequestStateValidateAcceptsKnownStates(t *testing.T) {
	t.Parallel()

	for _, state := range []RequestState{
		RequestStateIdle,
		RequestStatePending,
		RequestStateRunning,
		RequestStateCompleted,
		RequestStateFailed,
	} {
		if err := state.Validate(); err != nil {
			t.Fatalf("Validate(%q) error = %v, want nil", state, err)
		}
	}
}

func TestRequestStateValidateRejectsUnknown(t *testing.T) {
	t.Parallel()

	if err := RequestState("bogus").Validate(); err == nil {
		t.Fatal("Validate('bogus') error = nil, want non-nil")
	}
}

// --- test doubles ---

type completeScanCall struct {
	ingester string
	scanErr  string
}

type completeReindexCall struct {
	ingester   string
	reindexErr string
}

type recordingStatusRequestStore struct {
	scanRequests         []string
	reindexRequests      []string
	completeScanCalls    []completeScanCall
	completeReindexCalls []completeReindexCall
	claimScanResult      ScanRequest
	claimReindexResult   ReindexRequest
	scanErr              error
}

func (s *recordingStatusRequestStore) RequestScan(_ context.Context, ingester string, _ time.Time) error {
	s.scanRequests = append(s.scanRequests, ingester)
	return s.scanErr
}

func (s *recordingStatusRequestStore) ClaimScanRequest(_ context.Context, _ string, _ time.Time) (ScanRequest, error) {
	return s.claimScanResult, s.scanErr
}

func (s *recordingStatusRequestStore) CompleteScanRequest(_ context.Context, ingester string, _ time.Time, scanErr string) error {
	s.completeScanCalls = append(s.completeScanCalls, completeScanCall{ingester: ingester, scanErr: scanErr})
	return s.scanErr
}

func (s *recordingStatusRequestStore) RequestReindex(_ context.Context, ingester string, _ time.Time) error {
	s.reindexRequests = append(s.reindexRequests, ingester)
	return s.scanErr
}

func (s *recordingStatusRequestStore) ClaimReindexRequest(_ context.Context, _ string, _ time.Time) (ReindexRequest, error) {
	return s.claimReindexResult, s.scanErr
}

func (s *recordingStatusRequestStore) CompleteReindexRequest(_ context.Context, ingester string, _ time.Time, reindexErr string) error {
	s.completeReindexCalls = append(s.completeReindexCalls, completeReindexCall{ingester: ingester, reindexErr: reindexErr})
	return s.scanErr
}

func (s *recordingStatusRequestStore) GetScanState(_ context.Context, _ string) (ScanRequest, error) {
	return s.claimScanResult, s.scanErr
}

func (s *recordingStatusRequestStore) GetReindexState(_ context.Context, _ string) (ReindexRequest, error) {
	return s.claimReindexResult, s.scanErr
}
