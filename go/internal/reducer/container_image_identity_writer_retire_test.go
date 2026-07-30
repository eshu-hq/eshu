// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWriteContainerImageIdentityDecisionsCommitsPublicationAndLegacyCleanupAtomically(t *testing.T) {
	t.Parallel()

	tx := &containerImageIdentityRetireTx{retired: 1}
	beginner := &containerImageIdentityRetireBeginner{tx: tx}
	writer := PostgresContainerImageIdentityWriter{
		DB:       &containerImageIdentityRetireOutsideDB{},
		Beginner: beginner,
		Now: func() time.Time {
			return time.Date(2026, time.July, 29, 12, 0, 1, 0, time.UTC)
		},
	}
	write := containerImageIdentityRetireWrite()

	result, err := writer.WriteContainerImageIdentityDecisions(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v", err)
	}
	if got, want := beginner.calls, 1; got != want {
		t.Fatalf("begin calls = %d, want %d", got, want)
	}
	if got, want := len(tx.queries), 2; got != want {
		t.Fatalf("transaction queries = %d, want %d", got, want)
	}
	if tx.queries[0] != reducerFactBatchInsertQuery {
		t.Fatalf("first transaction query is not reducer batch insert:\n%s", tx.queries[0])
	}
	if tx.queries[1] != containerImageIdentityLegacyCleanupQuery {
		t.Fatalf("second transaction query is not legacy cleanup:\n%s", tx.queries[1])
	}
	if got, want := tx.args[1][0], write.LegacyFactIDs; !equalRetireIDArgument(got, want) {
		t.Fatalf("legacy IDs arg = %#v, want %#v", got, want)
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want true/false", tx.committed, tx.rolledBack)
	}
	if got, want := result.RetirementAttempts, 1; got != want {
		t.Fatalf("result.RetirementAttempts = %d, want %d tombstone publications attempted", got, want)
	}
	if got, want := result.LegacyRowsDeleted, 1; got != want {
		t.Fatalf("result.LegacyRowsDeleted = %d, want %d", got, want)
	}
}

func TestWriteContainerImageIdentityDecisionsRollsBackBeforeRetireWhenInsertFails(t *testing.T) {
	t.Parallel()

	tx := &containerImageIdentityRetireTx{failQuery: reducerFactBatchInsertQuery}
	writer := PostgresContainerImageIdentityWriter{
		DB:       &containerImageIdentityRetireOutsideDB{},
		Beginner: &containerImageIdentityRetireBeginner{tx: tx},
	}

	_, err := writer.WriteContainerImageIdentityDecisions(
		context.Background(),
		containerImageIdentityRetireWrite(),
	)
	if err == nil || !strings.Contains(err.Error(), "write container image identity fact") {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want insert failure", err)
	}
	if got, want := len(tx.queries), 1; got != want {
		t.Fatalf("transaction queries = %d, want %d (no retire after failed insert)", got, want)
	}
	if tx.committed || !tx.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want false/true", tx.committed, tx.rolledBack)
	}
}

func TestWriteContainerImageIdentityDecisionsRollsBackInsertWhenRetireFails(t *testing.T) {
	t.Parallel()

	tx := &containerImageIdentityRetireTx{failQuery: containerImageIdentityLegacyCleanupQuery}
	writer := PostgresContainerImageIdentityWriter{
		DB:       &containerImageIdentityRetireOutsideDB{},
		Beginner: &containerImageIdentityRetireBeginner{tx: tx},
	}

	_, err := writer.WriteContainerImageIdentityDecisions(
		context.Background(),
		containerImageIdentityRetireWrite(),
	)
	if err == nil || !strings.Contains(err.Error(), "delete legacy container image identity facts") {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want legacy cleanup failure", err)
	}
	if tx.committed || !tx.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want false/true", tx.committed, tx.rolledBack)
	}
}

func containerImageIdentityRetireWrite() ContainerImageIdentityWrite {
	decision := ContainerImageIdentityDecision{
		ImageRef:        "registry.example.com/team/api:prod",
		Digest:          retirementTestDigest,
		RepositoryID:    retirementTestRepositoryID,
		Outcome:         ContainerImageIdentityUnresolved,
		CanonicalWrites: 0,
	}
	return ContainerImageIdentityWrite{
		IntentID:           "intent-5854",
		ScopeID:            "repository:synthetic",
		GenerationID:       "generation-5854",
		SourceSystem:       "git",
		Cause:              "test",
		EvidenceAsOf:       time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC),
		Decisions:          []ContainerImageIdentityDecision{decision},
		TombstoneDecisions: []ContainerImageIdentityDecision{decision},
		LegacyFactIDs:      []string{"reducer_container_image_identity:synthetic-prior"},
	}
}

type containerImageIdentityRetireBeginner struct {
	tx    ContainerImageIdentityTransaction
	calls int
}

func (b *containerImageIdentityRetireBeginner) BeginContainerImageIdentityTx(
	context.Context,
) (ContainerImageIdentityTransaction, error) {
	b.calls++
	return b.tx, nil
}

type containerImageIdentityRetireOutsideDB struct{}

func (*containerImageIdentityRetireOutsideDB) ExecContext(
	context.Context,
	string,
	...any,
) (sql.Result, error) {
	return nil, errors.New("write escaped container image identity transaction")
}

type containerImageIdentityRetireTx struct {
	queries    []string
	args       [][]any
	failQuery  string
	retired    int64
	committed  bool
	rolledBack bool
}

func (tx *containerImageIdentityRetireTx) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	tx.queries = append(tx.queries, query)
	tx.args = append(tx.args, append([]any(nil), args...))
	if query == tx.failQuery {
		return nil, errors.New("synthetic transaction failure")
	}
	if query == containerImageIdentityLegacyCleanupQuery {
		return containerImageIdentityRetireResult(tx.retired), nil
	}
	return containerImageIdentityRetireResult(0), nil
}

func (tx *containerImageIdentityRetireTx) Commit() error {
	tx.committed = true
	return nil
}

func (tx *containerImageIdentityRetireTx) Rollback() error {
	tx.rolledBack = true
	return nil
}

type containerImageIdentityRetireResult int64

func (r containerImageIdentityRetireResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (r containerImageIdentityRetireResult) RowsAffected() (int64, error) {
	return int64(r), nil
}

func equalRetireIDArgument(got any, want []string) bool {
	gotStrings, ok := got.([]string)
	if !ok || len(gotStrings) != len(want) {
		return false
	}
	for index := range gotStrings {
		if gotStrings[index] != want[index] {
			return false
		}
	}
	return true
}
