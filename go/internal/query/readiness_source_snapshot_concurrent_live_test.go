// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9227 disposable PostgreSQL API proof that selector and committed snapshot are read in one repeatable-read transaction during reconfiguration
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type readinessPausedReadStore struct {
	db.ReadStore
	beforeRead chan struct{}
	resume     chan struct{}
}

func (s readinessPausedReadStore) BeginReadOnlySnapshot(ctx context.Context) (db.ReadTransaction, error) {
	tx, err := s.ReadStore.BeginReadOnlySnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return readinessPausedTransaction{ReadTransaction: tx, beforeRead: s.beforeRead, resume: s.resume}, nil
}

type readinessPausedTransaction struct {
	db.ReadTransaction
	beforeRead chan struct{}
	resume     chan struct{}
}

func (tx readinessPausedTransaction) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if strings.Contains(query, "vulnerability_source_state_candidates AS (") {
		close(tx.beforeRead)
		select {
		case <-tx.resume:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return tx.ReadTransaction.QueryContext(ctx, query, args...)
}

func TestSupplyChainImpactReadinessConcurrentReconfigurationUsesOneSnapshotLive(t *testing.T) {
	ctx, database, now := readinessScopeLiveDatabase(t)
	seedReadinessScopeInstance(t, ctx, database, now, "concurrent-instance", []map[string]any{{
		"source": "nvd", "scope_id": "concurrent-scope", "cve_ids": []string{"CVE-2026-9227"},
	}})
	seedReadinessScopeState(t, ctx, database, now, "concurrent-instance", "concurrent-scope", "nvd", "fresh", "succeeded")
	seedReadinessNVDCommittedSnapshot(t, ctx, database, now, "concurrent-scope", "concurrent-instance", "CVE-2026-9227")
	paused := readinessPausedReadStore{
		ReadStore:  storagepostgres.NewSQLReadStore(database),
		beforeRead: make(chan struct{}), resume: make(chan struct{}),
	}
	handler := &SupplyChainHandler{
		ImpactFindings: &recordingSupplyChainImpactFindingStore{},
		Readiness:      impact.NewPostgresReadinessStoreWithReadStore(paused),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-9227&limit=10", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() { mux.ServeHTTP(response, request); close(done) }()
	select {
	case <-paused.beforeRead:
	case <-time.After(10 * time.Second):
		t.Fatal("readiness query did not reach snapshot pause")
	}
	changed, err := json.Marshal(map[string]any{"targets": []map[string]any{{
		"source": "nvd", "scope_id": "concurrent-scope", "cve_ids": []string{"CVE-2026-9228"},
	}}})
	if err != nil {
		t.Fatalf("marshal concurrent reconfiguration: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration = $1
        WHERE instance_id = 'concurrent-instance'`, changed); err != nil {
		t.Fatalf("commit concurrent reconfiguration: %v", err)
	}
	close(paused.resume)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("readiness query did not complete")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Readiness impact.ReadinessEnvelope `json:"readiness"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readiness: %v", err)
	}
	if body.Readiness.State != impact.ReadinessStateReadyZeroFindings {
		t.Fatalf("one repeatable-read snapshot lost A coverage: %+v", body.Readiness)
	}
	current := readReadinessScopeAPI(t, database, "CVE-2026-9228")
	if current.State != impact.ReadinessStateTargetIncomplete {
		t.Fatalf("new transaction inherited A coverage for B: %+v", current)
	}
}
