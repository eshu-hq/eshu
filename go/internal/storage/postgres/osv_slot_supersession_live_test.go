// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9432 proof that two consecutive windows over one pinned _slot scope leave exactly one active generation: window one's generation superseded, window two's active with active_generation_id pointing at it, and the active fact union equal to window two's targets; untagged and gated by the projector claim proof DSN env Skip; Postgres only; about 5s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DISPOSABLE ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_POSTGRES_TEST_DSN ESHU_POSTGRES_DISPOSABLE

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
)

// seedSlotScopeFacts records one advisory fact per target name under the
// given generation of a _slot scope, standing in for the facts the OSV
// projector wrote for one planning window.
func seedSlotScopeFacts(t *testing.T, database *sql.DB, scopeID, generationID string, at time.Time, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := database.Exec(`
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at
) VALUES ($1, $2, $3, 'osv_advisory', $4, 'osv', $4, $5, $5)
`, "fact-"+generationID+"-"+name, scopeID, generationID, name, at); err != nil {
			t.Fatalf("insert fact %s under %s: %v", name, generationID, err)
		}
	}
}

// activeSlotFactKeys returns the stable keys visible through the scope's
// active generation pointer, the same join the read surfaces use.
func activeSlotFactKeys(t *testing.T, database *sql.DB, scopeID string) []string {
	t.Helper()
	rows, err := database.Query(`
SELECT stable_fact_key FROM fact_records
WHERE scope_id = $1
  AND generation_id = (SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1)
ORDER BY stable_fact_key
`, scopeID)
	if err != nil {
		t.Fatalf("read active facts for %s: %v", scopeID, err)
	}
	defer func() { _ = rows.Close() }()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatalf("scan active fact for %s: %v", scopeID, err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate active facts for %s: %v", scopeID, err)
	}
	return keys
}

// TestProjectorAckSupersedesSlotScopePredecessor proves the #9432 cutover
// contract at the storage layer through the shipped projector statements:
// two consecutive windows mint the same pinned _slot scope id with different
// targets, and after both generations commit and ack exactly one active
// generation remains per slot scope, the scope's active pointer names window
// two's generation, window one's generation reads superseded, and the active
// fact union equals window two's targets.
func TestProjectorAckSupersedesSlotScopePredecessor(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	ctx := context.Background()
	database := openClaimDeadlockProofDB(t, dsn, 2)
	at := time.Now().UTC().Truncate(time.Second)
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	queue.Now = func() time.Time { return at }

	slotScope := "vuln-intel://osv/npm/_slot/7/0"
	otherScope := "vuln-intel://osv/npm/_slot/500/0"
	seedClaimMaintenanceScopes(t, database, slotScope, otherScope)

	// Window one: aaa and mmm share virtual slot 7; window two drops mmm
	// for bbb in the same slot. The planner-level stability test proves both
	// windows mint the same scope set; here each window commits its own
	// generation and facts.
	seedFenceProofWork(t, database, fenceProofWork{
		slotScope, "gen-slot-1", "pending", "pending", 0,
		at.Add(-time.Hour), at.Add(-time.Hour), at.Add(-time.Hour),
	})
	seedSlotScopeFacts(t, database, slotScope, "gen-slot-1", at, "npm:left-pad@1.3.0", "npm:is-odd@3.0.1")

	work, ok, err := queue.Claim(ctx)
	if err != nil || !ok || work.Generation.GenerationID != "gen-slot-1" {
		t.Fatalf("Claim() = (%q, %v, %v), want gen-slot-1", work.Generation.GenerationID, ok, err)
	}
	if err := queue.Ack(ctx, work, runtime.Result{}); err != nil {
		t.Fatalf("Ack(gen-slot-1) = %v, want success", err)
	}

	seedFenceProofWork(t, database, fenceProofWork{
		slotScope, "gen-slot-2", "pending", "pending", 0,
		at, at, at,
	})
	seedSlotScopeFacts(t, database, slotScope, "gen-slot-2", at, "npm:left-pad@1.3.0", "npm:is-bbb@1.1.0")
	seedFenceProofWork(t, database, fenceProofWork{
		otherScope, "gen-other-1", "pending", "pending", 0,
		at, at, at,
	})

	work, ok, err = queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("Claim() second = (%q, %v, %v), want a second claim", work.Generation.GenerationID, ok, err)
	}
	secondID := work.Generation.GenerationID
	if err := queue.Ack(ctx, work, runtime.Result{}); err != nil {
		t.Fatalf("Ack(%s) = %v, want success", secondID, err)
	}
	work, ok, err = queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("Claim() third = (%q, %v, %v), want a third claim", work.Generation.GenerationID, ok, err)
	}
	thirdID := work.Generation.GenerationID
	if err := queue.Ack(ctx, work, runtime.Result{}); err != nil {
		t.Fatalf("Ack(%s) = %v, want success", thirdID, err)
	}

	if got := generationState(t, database, "gen-slot-1"); got != "superseded" {
		t.Fatalf("gen-slot-1 generation = %q, want superseded", got)
	}
	if got := generationState(t, database, "gen-slot-2"); got != "active" {
		t.Fatalf("gen-slot-2 generation = %q, want active", got)
	}
	var activePointer string
	if err := database.QueryRow(`SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1`,
		slotScope).Scan(&activePointer); err != nil {
		t.Fatalf("read active pointer for %s: %v", slotScope, err)
	}
	if activePointer != "gen-slot-2" {
		t.Fatalf("active_generation_id = %q, want window two's gen-slot-2", activePointer)
	}
	// The QA census shape: one active generation per slot scope.
	rows, err := database.Query(`
SELECT scope_id, count(*) FROM scope_generations
WHERE status = 'active' AND scope_id LIKE 'vuln-intel://osv/%/_slot/%'
GROUP BY scope_id ORDER BY scope_id
`)
	if err != nil {
		t.Fatalf("census active slot generations: %v", err)
	}
	defer func() { _ = rows.Close() }()
	census := map[string]int{}
	for rows.Next() {
		var scope string
		var count int
		if err := rows.Scan(&scope, &count); err != nil {
			t.Fatalf("scan census row: %v", err)
		}
		census[scope] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate census rows: %v", err)
	}
	for scope, count := range census {
		if count != 1 {
			t.Fatalf("active generations for %s = %d, want exactly one per slot scope", scope, count)
		}
	}
	if len(census) != 2 {
		t.Fatalf("active slot scopes = %v, want exactly the two seeded scopes", census)
	}
	got := activeSlotFactKeys(t, database, slotScope)
	if len(got) != 2 || got[0] != "npm:is-bbb@1.1.0" || got[1] != "npm:left-pad@1.3.0" {
		t.Fatalf("active fact union = %q, want window two's targets", got)
	}
}
