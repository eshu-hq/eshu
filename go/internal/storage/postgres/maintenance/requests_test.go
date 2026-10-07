// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenancestore_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/maintenance"
)

func TestStatusRequestStoreRequestScanExecutesUpsert(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{}
	store := maintenancestore.NewStatusRequestStore(database)
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)

	if err := store.RequestScan(context.Background(), "ingester-1", now); err != nil {
		t.Fatalf("RequestScan() error = %v, want nil", err)
	}
	if got, want := len(database.Execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(database.Execs[0].Query, "scan_request_status = 'pending'") {
		t.Fatalf("query missing pending transition: %s", database.Execs[0].Query)
	}
}

func TestStatusRequestStoreClaimScanQueryReturnsScanRequest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	database := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{
			{Data: [][]any{{"ingester-1", "running", now, now}}},
		},
	}
	store := maintenancestore.NewStatusRequestStore(database)

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
	database := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{
			{Data: [][]any{}},
		},
	}
	store := maintenancestore.NewStatusRequestStore(database)

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

	database := &fake.ExecQueryer{}
	store := maintenancestore.NewStatusRequestStore(database)
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)

	if err := store.CompleteScanRequest(context.Background(), "ingester-1", now, ""); err != nil {
		t.Fatalf("CompleteScanRequest() error = %v, want nil", err)
	}
	if got, want := len(database.Execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(database.Execs[0].Query, "scan_request_status = CASE") {
		t.Fatalf("query missing status CASE: %s", database.Execs[0].Query)
	}
}

func TestStatusRequestStoreRequestReindexReturnsMonotonicDBWatermark(t *testing.T) {
	t.Parallel()

	stored := time.Date(2026, 4, 13, 12, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	database := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{{Data: [][]any{{stored}}}},
	}
	store := maintenancestore.NewStatusRequestStore(database)

	requestedAt, err := store.RequestReindex(context.Background(), "repository")
	if err != nil {
		t.Fatalf("RequestReindex() error = %v, want nil", err)
	}
	if !requestedAt.Equal(stored) || requestedAt.Location() != time.UTC {
		t.Fatalf("RequestReindex() = %v, want %v in UTC", requestedAt, stored.UTC())
	}
	if got, want := len(database.Queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if got := len(database.Execs); got != 0 {
		t.Fatalf("exec count = %d, want 0 (the watermark must be read back)", got)
	}
	query := database.Queries[0].Query
	for _, want := range []string{
		"reindex_request_status = 'pending'",
		"GREATEST(",
		"now()",
		"RETURNING reindex_request_requested_at",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("request reindex query missing %q: %s", want, query)
		}
	}
	if got, want := len(database.Queries[0].Args), 1; got != want {
		t.Fatalf("query args = %d, want %d (the timestamp must come from the database clock)", got, want)
	}
}

func TestStatusRequestStoreRequestReindexErrorsWhenNoRowReturned(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: [][]any{}}}}
	store := maintenancestore.NewStatusRequestStore(database)

	if _, err := store.RequestReindex(context.Background(), "repository"); err == nil {
		t.Fatal("RequestReindex() error = nil, want non-nil when RETURNING yields no row")
	}
}

func TestStatusRequestStoreGetScanStateReturnsIdleWhenNotFound(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{
			{Data: [][]any{}},
		},
	}
	store := maintenancestore.NewStatusRequestStore(database)

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

	database := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{
			{Data: [][]any{}},
		},
	}
	store := maintenancestore.NewStatusRequestStore(database)

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

	store := maintenancestore.NewStatusRequestStore(nil)

	if err := store.RequestScan(context.Background(), "ingester-1", time.Now()); err == nil {
		t.Fatal("RequestScan() error = nil, want non-nil")
	}
	if _, err := store.RequestReindex(context.Background(), "ingester-1"); err == nil {
		t.Fatal("RequestReindex() error = nil, want non-nil")
	}
}

func TestStatusRequestStoreControlSchemaIncludesExpectedColumns(t *testing.T) {
	t.Parallel()

	// The gocritic argOrder heuristic misfires on the qualified
	// maintenancestore.ControlSchemaSQL form inside strings.Contains
	// assertions; a bare identifier of the same name passes. See
	// go/internal/query/compat_supply_chain.go for the same workaround.
	controlSchemaSQL := maintenancestore.ControlSchemaSQL
	for _, want := range []string{
		"scan_request_status",
		"scan_request_requested_at",
		"reindex_request_status",
		"reindex_request_requested_at",
		"ingester TEXT PRIMARY KEY",
	} {
		if !strings.Contains(controlSchemaSQL, want) {
			t.Fatalf("ControlSchemaSQL missing %q", want)
		}
	}
}

func TestStatusRequestStoreBootstrapDefinitionRegistered(t *testing.T) {
	t.Parallel()

	var found bool
	for _, def := range postgres.BootstrapDefinitions() {
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
