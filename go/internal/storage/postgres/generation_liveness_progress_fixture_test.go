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
)

// progressFixture builds #7265 progress-window fixtures with every timestamp
// written as a literal relative to one Go anchor. The sweep is called with the
// same anchor as "now", so boundary cases one second either side of the
// progress window are exact instead of racing SQL now() against Go time.
type progressFixture struct {
	anchor time.Time
	seq    int
	sql    strings.Builder
}

func newProgressFixture() *progressFixture {
	return &progressFixture{anchor: time.Now().UTC().Truncate(time.Microsecond)}
}

// at renders anchor-ago as a timestamptz literal.
func (f *progressFixture) at(ago time.Duration) string {
	return "'" + f.anchor.Add(-ago).Format(time.RFC3339Nano) + "'::timestamptz"
}

// generation seeds scope-<name>/gen-<name> as the scope's active generation,
// activated activatedAgo before the anchor, with the normal succeeded
// source-local projector row the activation path leaves behind.
func (f *progressFixture) generation(name string, activatedAgo time.Duration) {
	scope, gen := "scope-"+name, "gen-"+name
	fmt.Fprintf(&f.sql, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ('%[1]s', 'repository', 'github', 'acme/%[3]s', 'git', 'acme/%[3]s', %[4]s, %[4]s, 'active', '%[2]s');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ('%[2]s', '%[1]s', 'push', %[4]s, %[4]s, 'active', %[4]s);
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, payload, created_at, updated_at)
VALUES ('projector_%[1]s_%[2]s', '%[1]s', '%[2]s', 'projector', 'source_local', 'succeeded', '{}'::jsonb, %[4]s, %[4]s);
`, scope, gen, name, f.at(activatedAgo))
}

// pending seeds one outstanding intent for gen-<name> in domain.
func (f *progressFixture) pending(name, domain, sourceRunID string) {
	f.seq++
	fmt.Fprintf(&f.sql, `
INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id, payload, created_at)
VALUES ('intent-pending-%[1]d', '%[2]s', 'p-%[1]d', 'scope-%[3]s', '', 'acme/%[3]s', '%[4]s', 'gen-%[3]s',
    '{"action":"upsert"}'::jsonb, %[5]s);
`, f.seq, domain, name, sourceRunID, f.at(90*time.Minute))
}

// completed seeds one intent in domain that completed ago before the anchor.
// It belongs to an unrelated generation: progress is per domain queue, not
// per generation.
func (f *progressFixture) completed(domain, sourceRunID string, ago time.Duration) {
	f.seq++
	fmt.Fprintf(&f.sql, `
INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id, payload, created_at, completed_at)
VALUES ('intent-done-%[1]d', '%[2]s', 'p-%[1]d', 'scope-other', '', 'acme/other', '%[3]s', 'gen-other-%[1]d',
    '{"action":"upsert"}'::jsonb, %[4]s, %[5]s);
`, f.seq, domain, sourceRunID, f.at(ago+time.Minute), f.at(ago))
}

// provision creates an isolated proof schema seeded with the fixture.
func (f *progressFixture) provision(t *testing.T, db *sql.DB) {
	t.Helper()
	provisionLivenessSchema(t, db, f.sql.String())
}

// projectorRecoveryState reads the canonical source-local projector row for
// scope-<name>/gen-<name>: its liveness_recovery_attempts (NULL when never
// re-driven) and its status.
func projectorRecoveryState(t *testing.T, db *sql.DB, name string) (sql.NullInt64, string) {
	t.Helper()
	var attempts sql.NullInt64
	var status string
	if err := db.QueryRowContext(context.Background(), `
		SELECT (payload ->> 'liveness_recovery_attempts')::bigint, status
		FROM fact_work_items
		WHERE work_item_id = $1
	`, "projector_scope-"+name+"_gen-"+name).Scan(&attempts, &status); err != nil {
		t.Fatalf("query projector row for %s: %v", name, err)
	}
	return attempts, status
}
