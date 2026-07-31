// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestWriteContainerImageIdentityDecisionsReusesCompletedSingleChunkCutover(
	t *testing.T,
) {
	t.Parallel()

	db := &containerImageIdentityRetireDirectDB{retired: 1, claimValid: true}
	beginner := &containerImageIdentityRetireBeginner{
		tx: &containerImageIdentityRetireTx{},
	}
	lookup := &containerImageIdentityRetireCutoverLookup{exists: true}
	cleanupLookup := &containerImageIdentityRetireCleanupLookup{}
	writer := PostgresContainerImageIdentityWriter{
		DB:                  db,
		Beginner:            beginner,
		CutoverLookup:       lookup,
		LegacyCleanupLookup: cleanupLookup,
		ClaimedExecer:       db,
	}

	result, err := writer.WriteContainerImageIdentityDecisions(
		context.Background(),
		containerImageIdentityRetireWrite(),
	)
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v", err)
	}
	if lookup.calls != 1 {
		t.Fatalf("cutover lookup calls = %d, want 1", lookup.calls)
	}
	if cleanupLookup.calls != 1 {
		t.Fatalf("legacy cleanup lookup calls = %d, want 1", cleanupLookup.calls)
	}
	if beginner.calls != 0 {
		t.Fatalf("transaction begin calls = %d, want 0 after completed cutover", beginner.calls)
	}
	if got := len(db.queries); got != 1 {
		t.Fatalf("direct queries = %d, want 1 publication+cleanup statement", got)
	}
	if db.queries[0] != containerImageIdentityCompletedCutoverWriteQuery {
		t.Fatalf("completed-cutover query is not exact-claim publication+cleanup:\n%s", db.queries[0])
	}
	if got, want := result.LegacyRowsDeleted, 1; got != want {
		t.Fatalf("LegacyRowsDeleted = %d, want %d", got, want)
	}
}

func TestWriteContainerImageIdentityDecisionsReusesCompletedMultiChunkCutover(
	t *testing.T,
) {
	t.Parallel()

	tx := &containerImageIdentityRetireTx{retired: 1, claimValid: true}
	beginner := &containerImageIdentityRetireBeginner{tx: tx}
	lookup := &containerImageIdentityRetireCutoverLookup{exists: true}
	cleanupLookup := &containerImageIdentityRetireCleanupLookup{}
	writer := PostgresContainerImageIdentityWriter{
		DB:                  &containerImageIdentityRetireOutsideDB{},
		Beginner:            beginner,
		CutoverLookup:       lookup,
		LegacyCleanupLookup: cleanupLookup,
	}

	result, err := writer.WriteContainerImageIdentityDecisions(
		context.Background(),
		containerImageIdentityRetireMultiChunkWrite(),
	)
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v", err)
	}
	if got, want := beginner.calls, 1; got != want {
		t.Fatalf("transaction begin calls = %d, want %d", got, want)
	}
	if got, want := len(tx.queries), 3; got != want {
		t.Fatalf("transaction queries = %d, want %d", got, want)
	}
	if tx.queries[0] != containerImageIdentityCompletedCutoverClaimLockQuery {
		t.Fatalf("first query = %q, want exact-claim lock", tx.queries[0])
	}
	if tx.queries[1] != reducerFactBatchInsertQuery {
		t.Fatalf("second query = %q, want bounded publication", tx.queries[1])
	}
	if tx.queries[2] != containerImageIdentityPublishAndLegacyCleanupQuery {
		t.Fatalf("third query = %q, want final publication+cleanup", tx.queries[2])
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want true/false", tx.committed, tx.rolledBack)
	}
	if got, want := result.LegacyRowsDeleted, 1; got != want {
		t.Fatalf("LegacyRowsDeleted = %d, want %d", got, want)
	}
}

func TestWriteContainerImageIdentityDecisionsSkipsProvenCompleteSingleChunkCleanup(
	t *testing.T,
) {
	t.Parallel()

	db := &containerImageIdentityRetireDirectDB{claimValid: true}
	writer := PostgresContainerImageIdentityWriter{
		DB:                  db,
		CutoverLookup:       &containerImageIdentityRetireCutoverLookup{exists: true},
		LegacyCleanupLookup: &containerImageIdentityRetireCleanupLookup{complete: true},
		ClaimedExecer:       db,
	}
	result, err := writer.WriteContainerImageIdentityDecisions(
		context.Background(),
		containerImageIdentityRetireWrite(),
	)
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v", err)
	}
	if got := len(db.queries); got != 1 {
		t.Fatalf("direct queries = %d, want 1 claim-bound publication", got)
	}
	if db.queries[0] != containerImageIdentityCompletedCutoverPublishOnlyQuery {
		t.Fatalf("completed zero-legacy query repeated cleanup:\n%s", db.queries[0])
	}
	if result.LegacyRowsDeleted != 0 {
		t.Fatalf("LegacyRowsDeleted = %d, want 0", result.LegacyRowsDeleted)
	}
}

func TestWriteContainerImageIdentityDecisionsSkipsProvenCompleteMultiChunkCleanup(
	t *testing.T,
) {
	t.Parallel()

	tx := &containerImageIdentityRetireTx{claimValid: true}
	writer := PostgresContainerImageIdentityWriter{
		DB:                  &containerImageIdentityRetireOutsideDB{},
		Beginner:            &containerImageIdentityRetireBeginner{tx: tx},
		CutoverLookup:       &containerImageIdentityRetireCutoverLookup{exists: true},
		LegacyCleanupLookup: &containerImageIdentityRetireCleanupLookup{complete: true},
	}
	result, err := writer.WriteContainerImageIdentityDecisions(
		context.Background(),
		containerImageIdentityRetireMultiChunkWrite(),
	)
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v", err)
	}
	if got, want := len(tx.queries), 3; got != want {
		t.Fatalf("transaction queries = %d, want %d", got, want)
	}
	if tx.queries[0] != containerImageIdentityCompletedCutoverClaimLockQuery {
		t.Fatalf("first query = %q, want exact-claim lock", tx.queries[0])
	}
	for index, query := range tx.queries[1:] {
		if query != reducerFactBatchInsertQuery {
			t.Fatalf("publication query %d = %q, want bounded insert", index, query)
		}
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want true/false", tx.committed, tx.rolledBack)
	}
	if result.LegacyRowsDeleted != 0 {
		t.Fatalf("LegacyRowsDeleted = %d, want 0", result.LegacyRowsDeleted)
	}
}

func TestWriteContainerImageIdentityDecisionsFailsClosedOnCutoverLookupError(
	t *testing.T,
) {
	t.Parallel()

	db := &containerImageIdentityRetireDirectDB{}
	beginner := &containerImageIdentityRetireBeginner{
		tx: &containerImageIdentityRetireTx{},
	}
	writer := PostgresContainerImageIdentityWriter{
		DB:       db,
		Beginner: beginner,
		CutoverLookup: &containerImageIdentityRetireCutoverLookup{
			err: errors.New("synthetic cutover lookup failure"),
		},
	}

	_, err := writer.WriteContainerImageIdentityDecisions(
		context.Background(),
		containerImageIdentityRetireWrite(),
	)
	if err == nil || !strings.Contains(err.Error(), "synthetic cutover lookup failure") {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want lookup failure", err)
	}
	if beginner.calls != 0 {
		t.Fatalf("transaction begin calls = %d, want 0", beginner.calls)
	}
	if len(db.queries) != 0 {
		t.Fatalf("write queries = %d, want 0", len(db.queries))
	}
}

func TestWriteContainerImageIdentityDecisionsFencesSingleChunkCleanupInTransaction(
	t *testing.T,
) {
	t.Parallel()

	tx := &containerImageIdentityRetireTx{retired: 1}
	beginner := &containerImageIdentityRetireBeginner{
		tx: tx,
	}
	writer := PostgresContainerImageIdentityWriter{
		DB:       &containerImageIdentityRetireOutsideDB{},
		Beginner: beginner,
	}

	result, err := writer.WriteContainerImageIdentityDecisions(
		context.Background(),
		containerImageIdentityRetireWrite(),
	)
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v", err)
	}
	if beginner.calls != 1 {
		t.Fatalf("transaction begin calls = %d, want 1 for the cutover fence", beginner.calls)
	}
	if got := len(tx.queries); got != 3 {
		t.Fatalf("transaction queries = %d, want fence, prelock, then publication+cleanup", got)
	}
	if tx.queries[0] != containerImageIdentityCutoverFenceQuery {
		t.Fatalf("first transaction query is not the cutover fence:\n%s", tx.queries[0])
	}
	if tx.queries[1] != containerImageIdentityLegacyPrelockQuery {
		t.Fatalf("second transaction query is not the legacy prelock:\n%s", tx.queries[1])
	}
	for _, want := range []string{"WITH published AS", "INSERT INTO fact_records", "DELETE FROM fact_records"} {
		if !strings.Contains(tx.queries[2], want) {
			t.Fatalf("single-chunk query missing %q:\n%s", want, tx.queries[2])
		}
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want true/false", tx.committed, tx.rolledBack)
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
	if got, want := len(tx.queries), 4; got != want {
		t.Fatalf("transaction queries = %d, want %d", got, want)
	}
	if tx.queries[0] != containerImageIdentityCutoverFenceQuery {
		t.Fatalf("first transaction query is not the cutover fence:\n%s", tx.queries[0])
	}
	if tx.queries[1] != containerImageIdentityLegacyPrelockQuery {
		t.Fatalf("second transaction query is not the legacy prelock:\n%s", tx.queries[1])
	}
	if tx.queries[2] != reducerFactBatchInsertQuery {
		t.Fatalf("third transaction query is not reducer batch insert:\n%s", tx.queries[2])
	}
	if tx.queries[3] != containerImageIdentityPublishAndLegacyCleanupQuery {
		t.Fatalf("fourth transaction query is not final publication+legacy cleanup:\n%s", tx.queries[3])
	}
	if got, want := tx.args[3][16], write.LegacyFactIDs; !equalRetireIDArgument(got, want) {
		t.Fatalf("legacy IDs arg = %#v, want %#v", got, want)
	}
	insertedFactIDs := append([]string(nil), tx.args[2][0].([]string)...)
	insertedFactIDs = append(insertedFactIDs, tx.args[3][0].([]string)...)
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
	if got, want := len(tx.queries), 3; got != want {
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
		ClaimEpoch:         1,
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
