package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/runtime"
)

func TestStatusRequestStoreRequestScanExecutesUpsert(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewStatusRequestStore(db)
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)

	if err := store.RequestScan(context.Background(), "ingester-1", now); err != nil {
		t.Fatalf("RequestScan() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "scan_request_status = 'pending'") {
		t.Fatalf("query missing pending transition: %s", db.execs[0].query)
	}
}

func TestStatusRequestStoreClaimScanQueryReturnsScanRequest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"ingester-1", "running", now, now}}},
		},
	}
	store := NewStatusRequestStore(db)

	result, err := store.ClaimScanRequest(context.Background(), "ingester-1", now)
	if err != nil {
		t.Fatalf("ClaimScanRequest() error = %v, want nil", err)
	}
	if got, want := result.Ingester, "ingester-1"; got != want {
		t.Fatalf("result.Ingester = %q, want %q", got, want)
	}
	if got, want := result.State, runtime.RequestStateRunning; got != want {
		t.Fatalf("result.State = %q, want %q", got, want)
	}
}

func TestStatusRequestStoreClaimScanReturnsErrorWhenNoPending(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{}},
		},
	}
	store := NewStatusRequestStore(db)

	_, err := store.ClaimScanRequest(context.Background(), "ingester-1", now)
	if err == nil {
		t.Fatal("ClaimScanRequest() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "no pending scan request") {
		t.Fatalf("error = %q, want 'no pending scan request'", err.Error())
	}
}

func TestStatusRequestStoreCompleteScanExecutesUpdate(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewStatusRequestStore(db)
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)

	if err := store.CompleteScanRequest(context.Background(), "ingester-1", now, ""); err != nil {
		t.Fatalf("CompleteScanRequest() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "scan_request_status = CASE") {
		t.Fatalf("query missing status CASE: %s", db.execs[0].query)
	}
}

func TestStatusRequestStoreRequestReindexExecutesUpsert(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewStatusRequestStore(db)
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)

	if err := store.RequestReindex(context.Background(), "ingester-1", now); err != nil {
		t.Fatalf("RequestReindex() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "reindex_request_status = 'pending'") {
		t.Fatalf("query missing pending transition: %s", db.execs[0].query)
	}
}

func TestStatusRequestStoreClaimReindexQueryReturnsReindexRequest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"ingester-1", "running", now, now}}},
		},
	}
	store := NewStatusRequestStore(db)

	result, err := store.ClaimReindexRequest(context.Background(), "ingester-1", now)
	if err != nil {
		t.Fatalf("ClaimReindexRequest() error = %v, want nil", err)
	}
	if got, want := result.Ingester, "ingester-1"; got != want {
		t.Fatalf("result.Ingester = %q, want %q", got, want)
	}
	if got, want := result.State, runtime.RequestStateRunning; got != want {
		t.Fatalf("result.State = %q, want %q", got, want)
	}
}

func TestStatusRequestStoreClaimReindexReturnsErrorWhenNoPending(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{}},
		},
	}
	store := NewStatusRequestStore(db)

	_, err := store.ClaimReindexRequest(context.Background(), "ingester-1", now)
	if err == nil {
		t.Fatal("ClaimReindexRequest() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "no pending reindex request") {
		t.Fatalf("error = %q, want 'no pending reindex request'", err.Error())
	}
}

func TestStatusRequestStoreGetScanStateReturnsIdleWhenNotFound(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{}},
		},
	}
	store := NewStatusRequestStore(db)

	result, err := store.GetScanState(context.Background(), "ingester-1")
	if err != nil {
		t.Fatalf("GetScanState() error = %v, want nil", err)
	}
	if got, want := result.State, runtime.RequestStateIdle; got != want {
		t.Fatalf("result.State = %q, want %q", got, want)
	}
	if got, want := result.Ingester, "ingester-1"; got != want {
		t.Fatalf("result.Ingester = %q, want %q", got, want)
	}
}

func TestStatusRequestStoreGetReindexStateReturnsIdleWhenNotFound(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{}},
		},
	}
	store := NewStatusRequestStore(db)

	result, err := store.GetReindexState(context.Background(), "ingester-1")
	if err != nil {
		t.Fatalf("GetReindexState() error = %v, want nil", err)
	}
	if got, want := result.State, runtime.RequestStateIdle; got != want {
		t.Fatalf("result.State = %q, want %q", got, want)
	}
}

func TestStatusRequestStoreRequiresDB(t *testing.T) {
	t.Parallel()

	store := NewStatusRequestStore(nil)

	if err := store.RequestScan(context.Background(), "ingester-1", time.Now()); err == nil {
		t.Fatal("RequestScan() error = nil, want non-nil")
	}
	if err := store.RequestReindex(context.Background(), "ingester-1", time.Now()); err == nil {
		t.Fatal("RequestReindex() error = nil, want non-nil")
	}
}

func TestStatusRequestStoreControlSchemaIncludesExpectedColumns(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"scan_request_status",
		"scan_request_requested_at",
		"reindex_request_status",
		"reindex_request_requested_at",
		"ingester TEXT PRIMARY KEY",
	} {
		if !strings.Contains(controlSchemaSQL, want) {
			t.Fatalf("controlSchemaSQL missing %q", want)
		}
	}
}

func TestStatusRequestStoreBootstrapDefinitionRegistered(t *testing.T) {
	t.Parallel()

	var found bool
	for _, def := range BootstrapDefinitions() {
		if def.Name == "runtime_ingester_control" {
			found = true
			if !strings.Contains(def.SQL, "runtime_ingester_control") {
				t.Fatal("definition SQL missing table name")
			}
			break
		}
	}
	if !found {
		t.Fatal("runtime_ingester_control not found in BootstrapDefinitions()")
	}
}
