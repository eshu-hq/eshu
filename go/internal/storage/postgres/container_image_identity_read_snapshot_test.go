// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func (f *fakeExecQueryer) BeginReadOnlyRepeatableRead(
	_ context.Context,
) (Transaction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beginReadOnlyRepeatableReadCalls++
	if f.beginReadOnlyRepeatableReadErr != nil {
		return nil, f.beginReadOnlyRepeatableReadErr
	}
	return &fakeReadOnlyRepeatableReadTransaction{parent: f}, nil
}

type fakeReadOnlyRepeatableReadTransaction struct {
	parent *fakeExecQueryer
}

func (tx *fakeReadOnlyRepeatableReadTransaction) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (Rows, error) {
	tx.parent.mu.Lock()
	tx.parent.transactionQueryCalls++
	tx.parent.mu.Unlock()
	return tx.parent.QueryContext(ctx, query, args...)
}

func (tx *fakeReadOnlyRepeatableReadTransaction) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	tx.parent.mu.Lock()
	tx.parent.transactionExecCalls++
	tx.parent.mu.Unlock()
	return tx.parent.ExecContext(ctx, query, args...)
}

func (tx *fakeReadOnlyRepeatableReadTransaction) Commit() error {
	tx.parent.mu.Lock()
	defer tx.parent.mu.Unlock()
	tx.parent.transactionCommitCalls++
	return tx.parent.transactionCommitErr
}

func (tx *fakeReadOnlyRepeatableReadTransaction) Rollback() error {
	tx.parent.mu.Lock()
	defer tx.parent.mu.Unlock()
	tx.parent.transactionRollbackCalls++
	return tx.parent.transactionRollbackErr
}

func TestActiveContainerImageIdentityLoadersUseOneReadSnapshot(t *testing.T) {
	tests := []struct {
		name        string
		responses   []queueFakeRows
		wantQueries int
		load        func(*FactStore) error
	}{
		{
			name:        "CI/CD identity pages",
			responses:   []queueFakeRows{{}},
			wantQueries: 1,
			load: func(store *FactStore) error {
				_, err := store.ListActiveCICDRunCorrelationFacts(
					context.Background(), []string{"sha256:snapshot"}, nil,
				)
				return err
			},
		},
		{
			name:        "SBOM legacy and identity streams",
			responses:   []queueFakeRows{{}, {}},
			wantQueries: 2,
			load: func(store *FactStore) error {
				_, err := store.ListActiveSBOMAttestationAttachmentFacts(
					context.Background(), []string{"sha256:snapshot"},
				)
				return err
			},
		},
		{
			name:        "supply chain page pairs",
			responses:   []queueFakeRows{{}},
			wantQueries: 1,
			load: func(store *FactStore) error {
				_, _, err := store.ListActiveSupplyChainImpactFacts(
					context.Background(),
					reducer.SupplyChainImpactFactFilter{SubjectDigests: []string{"sha256:snapshot"}},
				)
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := &fakeExecQueryer{queryResponses: test.responses}
			if err := test.load(NewFactStore(db)); err != nil {
				t.Fatalf("load active facts: %v", err)
			}
			if db.beginReadOnlyRepeatableReadCalls != 1 {
				t.Fatalf("read-only repeatable-read begins = %d, want 1", db.beginReadOnlyRepeatableReadCalls)
			}
			if db.transactionQueryCalls != test.wantQueries {
				t.Fatalf("transaction queries = %d, want %d", db.transactionQueryCalls, test.wantQueries)
			}
			if db.transactionCommitCalls != 1 || db.transactionRollbackCalls != 0 {
				t.Fatalf(
					"transaction completion = commit:%d rollback:%d, want 1/0",
					db.transactionCommitCalls, db.transactionRollbackCalls,
				)
			}
		})
	}
}

func TestActiveContainerImageIdentityLoaderFailsClosedWithoutReadSnapshot(t *testing.T) {
	inner := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}
	db := execQueryerWithoutReadSnapshot{ExecQueryer: inner}

	_, err := NewFactStore(db).ListActiveCICDRunCorrelationFacts(
		context.Background(), []string{"sha256:snapshot"}, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "read-only repeatable-read transactions") {
		t.Fatalf("loader error = %v, want missing read-snapshot capability", err)
	}
	if len(inner.queries) != 0 {
		t.Fatalf("queries without snapshot = %d, want 0", len(inner.queries))
	}
}

func TestActiveContainerImageIdentityLoaderRollsBackOnQueryFailure(t *testing.T) {
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{err: errors.New("query boom")}}}

	_, err := NewFactStore(db).ListActiveCICDRunCorrelationFacts(
		context.Background(), []string{"sha256:snapshot"}, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "query boom") {
		t.Fatalf("loader error = %v, want query failure", err)
	}
	if db.transactionCommitCalls != 0 || db.transactionRollbackCalls != 1 {
		t.Fatalf(
			"transaction completion = commit:%d rollback:%d, want 0/1",
			db.transactionCommitCalls, db.transactionRollbackCalls,
		)
	}
}

func TestActiveContainerImageIdentityLoaderReturnsBeginFailure(t *testing.T) {
	db := &fakeExecQueryer{beginReadOnlyRepeatableReadErr: errors.New("begin boom")}

	_, err := NewFactStore(db).ListActiveCICDRunCorrelationFacts(
		context.Background(), []string{"sha256:snapshot"}, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "begin boom") {
		t.Fatalf("loader error = %v, want begin failure", err)
	}
	if len(db.queries) != 0 || db.transactionCommitCalls != 0 || db.transactionRollbackCalls != 0 {
		t.Fatalf(
			"work after begin failure = queries:%d commit:%d rollback:%d, want 0/0/0",
			len(db.queries), db.transactionCommitCalls, db.transactionRollbackCalls,
		)
	}
}

func TestActiveContainerImageIdentityLoaderRollsBackOnCommitFailure(t *testing.T) {
	row := containerImageIdentitySupportFactRow(
		"repository:snapshot", "sha256:snapshot", 1,
		time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC),
	)
	db := &fakeExecQueryer{
		queryResponses:       []queueFakeRows{{rows: [][]any{row}}},
		transactionCommitErr: errors.New("commit boom"),
	}

	loaded, err := NewFactStore(db).ListActiveCICDRunCorrelationFacts(
		context.Background(), []string{"sha256:snapshot"}, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "commit boom") {
		t.Fatalf("loader error = %v, want commit failure", err)
	}
	if db.transactionCommitCalls != 1 || db.transactionRollbackCalls != 1 {
		t.Fatalf(
			"transaction completion = commit:%d rollback:%d, want 1/1",
			db.transactionCommitCalls, db.transactionRollbackCalls,
		)
	}
	if loaded != nil {
		t.Fatalf("loaded rows on failed commit = %d, want nil", len(loaded))
	}
}

func TestSupplyChainIdentityLoaderReturnsNoDataOnCommitFailure(t *testing.T) {
	row := taggedSupplyChainImpactRow(1, 1, containerImageIdentitySupportFactRow(
		"repository:snapshot", "sha256:snapshot", 1,
		time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC),
	))
	db := &fakeExecQueryer{
		queryResponses:       []queueFakeRows{{rows: [][]any{row}}},
		transactionCommitErr: errors.New("commit boom"),
	}

	loaded, truncated, err := NewFactStore(db).ListActiveSupplyChainImpactFacts(
		context.Background(),
		reducer.SupplyChainImpactFactFilter{SubjectDigests: []string{"sha256:snapshot"}},
	)
	if err == nil || !strings.Contains(err.Error(), "commit boom") {
		t.Fatalf("loader error = %v, want commit failure", err)
	}
	if loaded != nil || truncated {
		t.Fatalf("failed-commit result = rows:%d truncated:%t, want nil/false", len(loaded), truncated)
	}
}

func TestActiveContainerImageIdentityLoaderJoinsRollbackFailure(t *testing.T) {
	db := &fakeExecQueryer{
		queryResponses:         []queueFakeRows{{err: errors.New("query boom")}},
		transactionRollbackErr: errors.New("rollback boom"),
	}

	_, err := NewFactStore(db).ListActiveCICDRunCorrelationFacts(
		context.Background(), []string{"sha256:snapshot"}, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "query boom") ||
		!strings.Contains(err.Error(), "rollback boom") {
		t.Fatalf("loader error = %v, want joined query and rollback failures", err)
	}
}

func TestActiveContainerImageIdentityLoaderIgnoresAlreadyRolledBackCleanup(t *testing.T) {
	db := &fakeExecQueryer{
		queryResponses:         []queueFakeRows{{err: errors.New("query boom")}},
		transactionRollbackErr: sql.ErrTxDone,
	}

	_, err := NewFactStore(db).ListActiveCICDRunCorrelationFacts(
		context.Background(), []string{"sha256:snapshot"}, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "query boom") {
		t.Fatalf("loader error = %v, want query failure", err)
	}
	if strings.Contains(err.Error(), sql.ErrTxDone.Error()) {
		t.Fatalf("loader error = %v, must not report database/sql auto-rollback as a second failure", err)
	}
}

func TestSBOMReadSnapshotRollsBackOnCrossStreamCollision(t *testing.T) {
	row := containerImageIdentitySupportFactRow(
		"repository:snapshot", "sha256:snapshot", 1,
		time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC),
	)
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{row}},
		{rows: [][]any{row}},
	}}

	_, err := NewFactStore(db).ListActiveSBOMAttestationAttachmentFacts(
		context.Background(), []string{"sha256:snapshot"},
	)
	if err == nil || !strings.Contains(err.Error(), "duplicate fact ID") {
		t.Fatalf("loader error = %v, want cross-stream duplicate rejection", err)
	}
	if db.transactionCommitCalls != 0 || db.transactionRollbackCalls != 1 {
		t.Fatalf(
			"transaction completion = commit:%d rollback:%d, want 0/1",
			db.transactionCommitCalls, db.transactionRollbackCalls,
		)
	}
}

func TestActiveContainerImageIdentityEmptyFilterDoesNotBeginSnapshot(t *testing.T) {
	db := &fakeExecQueryer{}

	loaded, err := NewFactStore(db).ListActiveCICDRunCorrelationFacts(
		context.Background(), []string{"", "  "}, nil,
	)
	if err != nil {
		t.Fatalf("empty-filter load: %v", err)
	}
	if len(loaded) != 0 || db.beginReadOnlyRepeatableReadCalls != 0 || len(db.queries) != 0 {
		t.Fatalf(
			"empty-filter work = rows:%d begin:%d queries:%d, want 0/0/0",
			len(loaded), db.beginReadOnlyRepeatableReadCalls, len(db.queries),
		)
	}
}

type execQueryerWithoutReadSnapshot struct {
	ExecQueryer
}

var _ ExecQueryer = execQueryerWithoutReadSnapshot{}
