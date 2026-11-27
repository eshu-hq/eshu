// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// openPreC2IsolatedLiveDB is openIsolatedLiveDB for a proof that models a schema from before
// PR-C2 (#10880): it reads or plants finding facts in fact_records, which the write-stop
// trigger (migration 358) refuses, so the trigger is disabled for the whole test and re-armed
// (and checked) at cleanup. Named in the shrink-only allowlist in
// internal/testutil/postgresproof/finding_write_stop_allowlist_test.go.
func openPreC2IsolatedLiveDB(t *testing.T, prefix, skipMessage string) (*sql.DB, context.Context) {
	t.Helper()
	db, ctx := openIsolatedLiveDB(t, prefix, skipMessage)
	postgresproof.DisarmFindingWriteStopForTest(t, db)
	return db, ctx
}

// mirrorB2CSupportsToFactTwins plants one finding-fact twin per support the
// C2 writer wrote, so the frozen finding-fact oracles in the B2c reader
// equality proof keep reading the writer's rows on an armed database. The
// writer plants no finding facts there (#10880 PR-C2), so without the twins
// the oracle side would read back empty. Each twin carries the support's
// payload and stable_fact_key verbatim and the fact columns the in-file
// plant precedent (b2cOldWriterInsert) carries; the insert is missing-only
// (WHERE NOT EXISTS), so it never rewrites a twin. Call it once, after every
// impact write the oracles must see and before any id the seed mints: a twin
// planted before a retraction would survive it and the oracle would name a
// finding the supports read retracted.
func mirrorB2CSupportsToFactTwins(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) int64 {
	t.Helper()
	result, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  schema_version, collector_kind, fencing_token, source_confidence,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone,
  payload
)
SELECT s.fact_id, s.scope_id, s.generation_id,
  'reducer_supply_chain_impact_finding', s.stable_fact_key,
  '1.0.0', 'reducer', 0, 'exact',
  'vulnerability_intelligence', s.fact_id, $1, $1, FALSE, s.payload
FROM supply_chain_impact_finding_supports AS s
WHERE NOT EXISTS (SELECT 1 FROM fact_records AS f WHERE f.fact_id = s.fact_id)`,
		now,
	)
	if err != nil {
		t.Fatalf("mirror supports to fact twins: %v", err)
	}
	mirrored, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("mirror supports to fact twins rows: %v", err)
	}
	return mirrored
}
