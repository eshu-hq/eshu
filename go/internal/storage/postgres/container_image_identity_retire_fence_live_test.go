// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// The #5847 FENCE proofs: who may retire, as opposed to which rows a retire may
// touch. The partition-bounding and empty-keep-set proofs, plus the shared
// helpers both files use, live in container_image_identity_retire_live_test.go.

// TestContainerImageIdentityRetireCannotDeleteFresherEvidenceRowsLive is the
// #5847 fence proof, and the reason the fence exists at all.
//
// Worker B reclaims a lapsed lease, reads fresh evidence and writes. Worker A —
// the stalled holder, whose evidence predates B's by a lease — then lands. Its
// retire is generation-authoritative, so without the fencing token it DELETES
// B's correct row and leaves only its own stale one, which is strictly worse
// than the pre-retire behavior of leaving a stale row beside the correct one.
// With the token it deletes nothing of B's.
//
// The two writes deliberately classify the same image reference differently,
// which is what gives them different fact ids (the identity embeds outcome) and
// makes this a delete rather than an upsert.
func TestContainerImageIdentityRetireCannotDeleteFresherEvidenceRowsLive(t *testing.T) {
	sqlDB, ctx := containerImageIdentityRetireLiveDB(t)

	suffix := fmt.Sprintf("5847-fence-%d", time.Now().UnixNano())
	scopeID := "scope-" + suffix
	generationID := "gen-" + suffix
	seedContainerImageIdentityScopeGeneration(t, ctx, sqlDB, scopeID, generationID)

	staleEvidenceAsOf := time.Now().UTC().Add(-5 * time.Minute)
	freshEvidenceAsOf := time.Now().UTC()

	writer := reducer.PostgresContainerImageIdentityWriter{DB: sqlDB}

	// Worker B: fresher evidence, lands first.
	freshResult, err := writer.WriteContainerImageIdentityDecisions(ctx, containerImageIdentityLiveWrite(
		scopeID, generationID, suffix, freshEvidenceAsOf, reducer.ContainerImageIdentityExactDigest,
	))
	if err != nil {
		t.Fatalf("fresh write error = %v, want nil", err)
	}
	if freshResult.CanonicalWrites != 1 {
		t.Fatalf("fresh CanonicalWrites = %d, want 1", freshResult.CanonicalWrites)
	}

	var freshFactID string
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT fact_id FROM fact_records
		 WHERE scope_id = $1 AND generation_id = $2 AND payload->>'outcome' = $3`,
		scopeID, generationID, string(reducer.ContainerImageIdentityExactDigest),
	).Scan(&freshFactID); err != nil {
		t.Fatalf("read fresh writer's fact id: %v", err)
	}

	// Worker A: the stalled holder, older evidence, lands second.
	staleResult, err := writer.WriteContainerImageIdentityDecisions(ctx, containerImageIdentityLiveWrite(
		scopeID, generationID, suffix, staleEvidenceAsOf, reducer.ContainerImageIdentityTagResolved,
	))
	if err != nil {
		t.Fatalf("stale write error = %v, want nil", err)
	}
	if staleResult.Retired != 0 {
		t.Fatalf(
			"the stalled worker's retire deleted %d row(s) written from FRESHER evidence, want 0; "+
				"the fencing token on fact_records.fencing_token is not holding",
			staleResult.Retired,
		)
	}

	survivors := containerImageIdentitySurvivingFactIDs(t, ctx, sqlDB, []string{freshFactID})
	if len(survivors) != 1 {
		t.Fatal("the stalled worker's retire deleted the row written from FRESHER evidence; " +
			"the fencing token on fact_records.fencing_token is not holding")
	}

	// And the fresher row must still carry the fresher token — a stale pass must
	// not be able to downgrade it, or the NEXT stale pass would delete it.
	var storedToken int64
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT fencing_token FROM fact_records WHERE fact_id = $1`, freshFactID,
	).Scan(&storedToken); err != nil {
		t.Fatalf("read fresh row fencing token: %v", err)
	}
	if want := freshEvidenceAsOf.UnixMicro(); storedToken != want {
		t.Fatalf("fresh row fencing_token = %d, want %d; a stale pass downgraded it", storedToken, want)
	}
}

// containerImageIdentityInterleavingExecer runs a hook the first time the
// wrapped writer issues a statement matching trigger, BEFORE that statement
// reaches the database.
//
// It exists to make a two-worker interleaving deterministic against real
// Postgres using the real production statements: the hook fires between worker
// B's INSERT and worker B's retire, which is exactly the window a concurrent
// stalled worker can land in.
type containerImageIdentityInterleavingExecer struct {
	db      *sql.DB
	trigger string
	hook    func()
	fired   bool
}

func (e *containerImageIdentityInterleavingExecer) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	if !e.fired && e.hook != nil && strings.Contains(query, e.trigger) {
		e.fired = true
		e.hook()
	}
	return e.db.ExecContext(ctx, query, args...)
}

// TestContainerImageIdentityFreshlyInsertedRowIsFencedBeforeItIsVisibleLive is
// the regression for the hole the stamp-plus-delete CTE does NOT close on its
// own.
//
// The CTE stamps fact_records.fencing_token in the retire statement, which runs
// AFTER the insert and in a separate autocommit. If the insert leaves the row at
// the column default 0 — which it does whenever the insert's column list omits
// fencing_token — then between those two statements the row is COMMITTED and
// VISIBLE carrying token 0. A stalled worker's fenced retire landing in that
// window evaluates `0 <= $5` as true and deletes the fresher worker's row
// anyway. Worst case both rows vanish: A's retire kills B's unstamped row, then
// B's retire kills A's stamped one. That is strictly worse than leaving a stale
// row beside a correct one, which is the whole thing the fence exists to prevent.
//
// The fence-cut mutant cannot catch this, because the hole is in the INSERT
// path, not in the retire predicate. The fix is to stamp at birth: the batched
// insert carries the token, so the row is never visible at 0.
//
// The interleaving is driven deterministically rather than raced: worker B's
// execer runs worker A's ENTIRE write (insert plus fenced retire, at a STALE
// watermark) at the moment B is about to issue its own retire. Both workers use
// the production writer and the production statements.
func TestContainerImageIdentityFreshlyInsertedRowIsFencedBeforeItIsVisibleLive(t *testing.T) {
	sqlDB, ctx := containerImageIdentityRetireLiveDB(t)

	suffix := fmt.Sprintf("5847-birth-%d", time.Now().UnixNano())
	scopeID := "scope-" + suffix
	generationID := "gen-" + suffix
	seedContainerImageIdentityScopeGeneration(t, ctx, sqlDB, scopeID, generationID)

	staleEvidenceAsOf := time.Now().UTC().Add(-5 * time.Minute)
	freshEvidenceAsOf := time.Now().UTC()

	staleWriter := reducer.PostgresContainerImageIdentityWriter{DB: sqlDB}
	var staleErr error
	interleaved := &containerImageIdentityInterleavingExecer{
		db:      sqlDB,
		trigger: "WITH stamped AS",
		hook: func() {
			// Worker A, the stalled holder: it reads OLDER evidence and lands
			// here, after worker B's row is committed but before B has stamped it.
			_, staleErr = staleWriter.WriteContainerImageIdentityDecisions(ctx, containerImageIdentityLiveWrite(
				scopeID, generationID, suffix, staleEvidenceAsOf, reducer.ContainerImageIdentityTagResolved,
			))
		},
	}

	freshWriter := reducer.PostgresContainerImageIdentityWriter{DB: interleaved}
	freshResult, err := freshWriter.WriteContainerImageIdentityDecisions(ctx, containerImageIdentityLiveWrite(
		scopeID, generationID, suffix, freshEvidenceAsOf, reducer.ContainerImageIdentityExactDigest,
	))
	if err != nil {
		t.Fatalf("fresh write error = %v, want nil", err)
	}
	if staleErr != nil {
		t.Fatalf("stalled worker's write error = %v, want nil", staleErr)
	}
	if !interleaved.fired {
		t.Fatal("the interleaving hook never fired; this test proved nothing")
	}
	if freshResult.CanonicalWrites != 1 {
		t.Fatalf("fresh CanonicalWrites = %d, want 1", freshResult.CanonicalWrites)
	}

	var survivingFactIDs []string
	rows, err := sqlDB.QueryContext(ctx,
		`SELECT fact_id, fencing_token, payload->>'outcome' FROM fact_records
		 WHERE scope_id = $1 AND generation_id = $2 AND fact_kind = $3 ORDER BY fact_id`,
		scopeID, generationID, containerImageIdentityLiveFactKind)
	if err != nil {
		t.Fatalf("query surviving rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var outcomes []string
	var tokens []int64
	for rows.Next() {
		var factID, outcome string
		var token int64
		if err := rows.Scan(&factID, &token, &outcome); err != nil {
			t.Fatalf("scan surviving row: %v", err)
		}
		survivingFactIDs = append(survivingFactIDs, factID)
		outcomes = append(outcomes, outcome)
		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate surviving rows: %v", err)
	}

	if len(survivingFactIDs) != 1 {
		t.Fatalf(
			"surviving rows = %v (outcomes %v, tokens %v), want exactly 1: the fresher worker's row must survive "+
				"a stalled worker's retire that lands between its INSERT and its stamp",
			survivingFactIDs, outcomes, tokens,
		)
	}
	if outcomes[0] != string(reducer.ContainerImageIdentityExactDigest) {
		t.Fatalf(
			"surviving outcome = %q, want %q: the stalled worker's decision won",
			outcomes[0], reducer.ContainerImageIdentityExactDigest,
		)
	}
	if want := freshEvidenceAsOf.UnixMicro(); tokens[0] != want {
		t.Fatalf("surviving row fencing_token = %d, want %d (its own evidence watermark)", tokens[0], want)
	}
}
