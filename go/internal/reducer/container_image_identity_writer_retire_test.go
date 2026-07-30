// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestWriteContainerImageIdentityDecisionsUsesOneStatementForSingleChunkCleanup(
	t *testing.T,
) {
	t.Parallel()

	db := &containerImageIdentityRetireDirectDB{retired: 1}
	beginner := &containerImageIdentityRetireBeginner{
		tx: &containerImageIdentityRetireTx{},
	}
	writer := PostgresContainerImageIdentityWriter{
		DB:       db,
		Beginner: beginner,
	}

	result, err := writer.WriteContainerImageIdentityDecisions(
		context.Background(),
		containerImageIdentityRetireWrite(),
	)
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v", err)
	}
	if beginner.calls != 0 {
		t.Fatalf("transaction begin calls = %d, want 0 for one bounded chunk", beginner.calls)
	}
	if got := len(db.queries); got != 1 {
		t.Fatalf("direct queries = %d, want 1 atomic publication+cleanup statement", got)
	}
	for _, want := range []string{"WITH published AS", "INSERT INTO fact_records", "DELETE FROM fact_records"} {
		if !strings.Contains(db.queries[0], want) {
			t.Fatalf("single-chunk query missing %q:\n%s", want, db.queries[0])
		}
	}
	if got, want := result.LegacyRowsDeleted, 1; got != want {
		t.Fatalf("LegacyRowsDeleted = %d, want %d", got, want)
	}
}

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
	write := containerImageIdentityRetireMultiChunkWrite()

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
	if tx.queries[1] != containerImageIdentityPublishAndLegacyCleanupQuery {
		t.Fatalf("second transaction query is not final publication+legacy cleanup:\n%s", tx.queries[1])
	}
	if got, want := tx.args[1][16], write.LegacyFactIDs; !equalRetireIDArgument(got, want) {
		t.Fatalf("legacy IDs arg = %#v, want %#v", got, want)
	}
	insertedFactIDs := append([]string(nil), tx.args[0][0].([]string)...)
	insertedFactIDs = append(insertedFactIDs, tx.args[1][0].([]string)...)
	if got, want := len(insertedFactIDs), len(write.Decisions); got != want {
		t.Fatalf("inserted fact IDs = %d, want %d", got, want)
	}
	if !slices.IsSorted(insertedFactIDs) {
		t.Fatal("multi-chunk fact IDs are not globally sorted by conflict key")
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want true/false", tx.committed, tx.rolledBack)
	}
	if got, want := result.RetirementAttempts, len(write.TombstoneDecisions); got != want {
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
		containerImageIdentityRetireMultiChunkWrite(),
	)
	if err == nil || !strings.Contains(err.Error(), "batch insert reducer facts") {
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

	tx := &containerImageIdentityRetireTx{
		failQuery: containerImageIdentityPublishAndLegacyCleanupQuery,
	}
	writer := PostgresContainerImageIdentityWriter{
		DB:       &containerImageIdentityRetireOutsideDB{},
		Beginner: &containerImageIdentityRetireBeginner{tx: tx},
	}

	_, err := writer.WriteContainerImageIdentityDecisions(
		context.Background(),
		containerImageIdentityRetireMultiChunkWrite(),
	)
	if err == nil || !strings.Contains(err.Error(), "publish container image identities and delete legacy facts") {
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

func containerImageIdentityRetireMultiChunkWrite() ContainerImageIdentityWrite {
	write := containerImageIdentityRetireWrite()
	base := write.Decisions[0]
	for i := 1; i <= reducerFactBatchSize; i++ {
		decision := base
		decision.ImageRef = fmt.Sprintf("registry.example.com/team/api:retire-%04d", i)
		write.Decisions = append(write.Decisions, decision)
		write.TombstoneDecisions = append(write.TombstoneDecisions, decision)
		write.LegacyFactIDs = append(
			write.LegacyFactIDs,
			fmt.Sprintf("reducer_container_image_identity:synthetic-prior-%04d", i),
		)
	}
	return write
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

type containerImageIdentityRetireDirectDB struct {
	queries []string
	args    [][]any
	retired int64
}

func (db *containerImageIdentityRetireDirectDB) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	db.queries = append(db.queries, query)
	db.args = append(db.args, append([]any(nil), args...))
	return containerImageIdentityRetireResult(db.retired), nil
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
	if query == containerImageIdentityPublishAndLegacyCleanupQuery {
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
