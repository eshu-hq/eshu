// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// The #5847 WRITE-AMPLIFICATION proof: what the retire must NOT do to the rows
// the insert just wrote. The fence proofs (who may retire) live in
// container_image_identity_retire_fence_live_test.go; the partition-bounding and
// empty-keep-set proofs, plus the shared helpers, live in
// container_image_identity_retire_live_test.go.

// containerImageIdentityRowVersion reads one keep-set row's physical row version
// and its fencing token in a single snapshot.
//
// `xmin` is the transaction id that produced the row's CURRENT version. Postgres
// has no in-place UPDATE: any UPDATE that matches a row writes a NEW version
// with a new `xmin`, generating WAL and leaving the old version for vacuum, even
// when every assigned value is byte-identical to what was already there. So
// comparing `xmin` across a statement is the direct observation of whether that
// statement rewrote the row, which neither `RowsAffected` on the DELETE nor the
// stored token value can answer.
func containerImageIdentityRowVersion(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, generationID string,
) (factID string, xmin int64, fencingToken int64) {
	t.Helper()

	if err := db.QueryRowContext(ctx,
		`SELECT fact_id, xmin::text::bigint, fencing_token FROM fact_records
		 WHERE scope_id = $1 AND generation_id = $2 AND fact_kind = $3`,
		scopeID, generationID, containerImageIdentityLiveFactKind,
	).Scan(&factID, &xmin, &fencingToken); err != nil {
		t.Fatalf("read keep-set row version for %s/%s: %v", scopeID, generationID, err)
	}
	return factID, xmin, fencingToken
}

// TestContainerImageIdentityRetireDoesNotRewriteKeepSetRowsLive pins the retire
// as touching ONLY the rows it deletes.
//
// The retire used to lead with a `WITH stamped AS (UPDATE ... SET fencing_token
// = $5 ... WHERE fact_id = ANY($4) AND fencing_token <= $5)` CTE. Once the
// batched insert began binding the same token (`reducerFactBatchInsertQuery`
// carries fencing_token as a bound column and keeps it under a
// `fact_records.fencing_token <= EXCLUDED.fencing_token` conflict guard, which
// rejects a lower-token upsert whole rather than merging it), that CTE became a
// proven no-op: the keep-set is built from the exact rows just handed to the
// insert, so by retire time every one of them already carries
// `fencing_token >= $5`, and the only
// rows the guard can still match are the ones already sitting at exactly `$5`,
// which it then sets to `$5`.
//
// A no-op UPDATE is not a free UPDATE. Postgres wrote a second version of every
// keep-set row on every intent execution — doubled WAL, doubled dead tuples, and
// doubled vacuum pressure on this domain's hot write path — and nothing could
// see it: the committed cost budget counts STATEMENTS, and the count stayed at
// two. This test counts row VERSIONS instead, which is the unit the cost was
// actually paid in.
//
// The hook fires between the insert and the retire, which is the only moment the
// freshly inserted row can be sampled before the retire has a chance to touch
// it. That sample also pins the second half of the property: the token is
// already this write's own watermark BEFORE the retire runs, so the stamp comes
// from the INSERT and dropping the CTE loses nothing.
func TestContainerImageIdentityRetireDoesNotRewriteKeepSetRowsLive(t *testing.T) {
	sqlDB, ctx := containerImageIdentityRetireLiveDB(t)

	suffix := fmt.Sprintf("5847-rowversion-%d", time.Now().UnixNano())
	scopeID := "scope-" + suffix
	generationID := "gen-" + suffix
	seedContainerImageIdentityScopeGeneration(t, ctx, sqlDB, scopeID, generationID)

	evidenceAsOf := time.Now().UTC()

	var (
		factIDBefore string
		xminBefore   int64
		tokenBefore  int64
	)
	probe := &containerImageIdentityInterleavingExecer{
		db:      sqlDB,
		trigger: containerImageIdentityRetireTrigger,
		hook: func() {
			factIDBefore, xminBefore, tokenBefore = containerImageIdentityRowVersion(
				t, ctx, sqlDB, scopeID, generationID)
		},
	}

	writer := reducer.PostgresContainerImageIdentityWriter{DB: probe}
	result, err := writer.WriteContainerImageIdentityDecisions(ctx, containerImageIdentityLiveWrite(
		scopeID, generationID, suffix, evidenceAsOf, reducer.ContainerImageIdentityExactDigest,
	))
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}
	if !probe.fired {
		t.Fatalf(
			"the retire probe never fired on %q; this test proved nothing",
			containerImageIdentityRetireTrigger,
		)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}

	// The INSERT, not the retire, is what stamps a keep-set row. Sampled before
	// the retire ran, so a passing assertion here cannot be the retire's doing.
	if want := evidenceAsOf.UnixMicro(); tokenBefore != want {
		t.Fatalf(
			"keep-set row fencing_token before the retire = %d, want %d; "+
				"the INSERT must stamp the row at birth, never the retire",
			tokenBefore, want,
		)
	}

	factIDAfter, xminAfter, tokenAfter := containerImageIdentityRowVersion(
		t, ctx, sqlDB, scopeID, generationID)
	if factIDAfter != factIDBefore {
		t.Fatalf("keep-set fact_id = %q after the retire, want %q", factIDAfter, factIDBefore)
	}
	if tokenAfter != tokenBefore {
		t.Fatalf("keep-set fencing_token = %d after the retire, want %d (unchanged)", tokenAfter, tokenBefore)
	}
	if xminAfter != xminBefore {
		t.Fatalf(
			"keep-set row xmin moved %d -> %d: the retire rewrote a row it was only meant to keep. "+
				"Every intent execution then costs a second row version per canonical decision — "+
				"WAL, bloat, and vacuum the statement-counting cost budget cannot see.",
			xminBefore, xminAfter,
		)
	}
}
