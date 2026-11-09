// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// fenceWrite builds a writer input for the generation-fence proof: a strictly
// increasing fencing token per write, like the production sequence.
func fenceWrite(intentID, scope, generation string, findings ...supplychaincore.SupplyChainImpactFinding) supplychaincore.SupplyChainImpactWrite {
	return supplychaincore.SupplyChainImpactWrite{
		FencingToken: replaceSetNextToken(),
		IntentID:     intentID,
		ScopeID:      scope,
		GenerationID: generation,
		SourceSystem: "vulnerability_intelligence",
		Cause:        "synthetic #10422 fence proof",
		Findings:     findings,
	}
}

// advanceFenceScope supersedes the scope's active generation and activates a
// newer one with a later ingested_at, mimicking the projector's activation:
// the older generation is stale from here on, whatever token a late pass
// draws.
func advanceFenceScope(t *testing.T, ctx context.Context, db *sql.DB, scope, older, newer string, at time.Time) {
	t.Helper()
	// Supersede first: one generation per scope may hold active status
	// (scope_generations_active_scope_idx).
	if _, err := db.ExecContext(ctx, `UPDATE scope_generations
SET status = 'superseded', superseded_at = $3
WHERE generation_id = $1 AND scope_id = $2`, older, scope, at); err != nil {
		t.Fatalf("supersede %s: %v", older, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO scope_generations
(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', $3, $3, 'active', $3, '{}'::jsonb)`, newer, scope, at); err != nil {
		t.Fatalf("activate %s: %v", newer, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scope, newer); err != nil {
		t.Fatalf("activate %s on scope: %v", newer, err)
	}
}

// fencePassState reads the scope's durable pass-state row: partial flag,
// causes text, and fencing token. The causes read as text to stay off
// driver array decoding. No row reports recorded=false.
func fencePassState(t *testing.T, ctx context.Context, db *sql.DB, scope string) (recorded, partial bool, causes string, token int64) {
	t.Helper()
	err := db.QueryRowContext(ctx, `SELECT partial_evidence, partial_causes::text, fencing_token
FROM supply_chain_impact_scope_pass_state WHERE scope_id = $1`, scope).Scan(&partial, &causes, &token)
	if err == sql.ErrNoRows {
		return false, false, "", 0
	}
	if err != nil {
		t.Fatalf("read pass state: %v", err)
	}
	return true, partial, causes, token
}

// TestStaleGenerationPassPreservesScopePassStateLive proves #10422 against
// real SQL: a stale generation's late pass draws a higher fencing token (tokens
// are per-pass monotonic, not per-generation), is admitted under its own
// generation, and must still leave the scope's pass-state row untouched once a
// lifecycle-newer generation is reader-visible. The lifecycle order is
// (ingested_at, generation_id), the order the supersede sweep, the freshness
// guard, and the projector use.
func TestStaleGenerationPassPreservesScopePassStateLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	scope := "fence-scope"
	fanOutSeedScope(t, ctx, db, scope, "gen-1")
	gen1 := fanOutGen(scope, "gen-1")
	gen2 := fanOutGen(scope, "gen-2")

	writer := supplychaincore.PostgresSupplyChainImpactWriter{
		DB:  postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}},
		Now: func() time.Time { return replaceSetLiveNow },
	}
	keep := fallbackFinding("CVE-2026-9101", "pkg:npm/a@1")

	// The current generation marks the scope partial: no newer generation
	// exists yet, so the record lands.
	stale := fenceWrite("intent:fence:gen1-partial", scope, gen1, keep)
	stale.PartialEvidence = true
	stale.PartialCauses = []string{"evidence_budget"}
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, stale); err != nil {
		t.Fatalf("gen-1 partial write: %v", err)
	}
	recorded, partial, _, _ := fencePassState(t, ctx, db, scope)
	if !recorded || !partial {
		t.Fatalf("gen-1 partial recorded=%v partial=%v, want recorded partial (no newer generation yet)", recorded, partial)
	}

	// The lifecycle advances: gen-2 ingests after the seed's clock
	// (fanOutSeedScope stamps 2026-10-31), so it is lifecycle-newer and
	// gen-1 is stale.
	advanceFenceScope(t, ctx, db, scope, gen1, gen2, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))

	// The newer generation records and clears: the fence must let the
	// lifecycle-newest generation through, and the result must not report
	// a skip.
	gen2Res, err := writer.WriteSupplyChainImpactFindings(ctx, fenceWrite("intent:fence:gen2-complete", scope, gen2, keep))
	if err != nil {
		t.Fatalf("gen-2 complete write: %v", err)
	}
	if gen2Res.PassStateSkipped {
		t.Fatalf("gen-2 complete PassStateSkipped=true, want false (lifecycle-newest records)")
	}
	if recorded, partial, _, _ := fencePassState(t, ctx, db, scope); !recorded || partial {
		t.Fatalf("gen-2 complete recorded=%v partial=%v, want recorded clear", recorded, partial)
	}

	// The stale generation replays late with a HIGHER fencing token. The
	// token guard passes (monotonic per pass), admission passes (per
	// scope+generation), but the row must stay clear and the result must
	// report the fence skip.
	replay := fenceWrite("intent:fence:gen1-replay", scope, gen1, keep)
	replay.PartialEvidence = true
	replay.PartialCauses = []string{"evidence_budget"}
	replayRes, err := writer.WriteSupplyChainImpactFindings(ctx, replay)
	if err != nil {
		t.Fatalf("stale gen-1 replay write: %v", err)
	}
	if !replayRes.PassStateSkipped {
		t.Fatalf("stale gen-1 replay PassStateSkipped=false, want true (fence held)")
	}
	if recorded, partial, causes, _ := fencePassState(t, ctx, db, scope); !recorded || partial {
		t.Fatalf("stale replay recorded=%v partial=%v causes=%v, want the gen-2 clear row untouched", recorded, partial, causes)
	}

	// Absent lineage cannot touch the row: for a generation with no
	// scope_generations row the bulk findings insert fails its
	// scope_generations foreign key first and the transaction aborts before
	// the record step (which runs last) ever executes. The gen-2 clear row
	// survives, no skip is reported, and the write returns the error.
	unknown := fenceWrite("intent:fence:genx-partial", scope, fanOutGen(scope, "gen-x"), keep)
	unknown.PartialEvidence = true
	unknown.PartialCauses = []string{"evidence_budget"}
	unknownRes, err := writer.WriteSupplyChainImpactFindings(ctx, unknown)
	if err == nil {
		t.Fatalf("unknown-generation write succeeded, want the findings foreign key to reject it")
	}
	if unknownRes.PassStateSkipped {
		t.Fatalf("unknown-generation PassStateSkipped=true, want false (the record step never ran)")
	}
	if recorded, partial, causes, _ := fencePassState(t, ctx, db, scope); !recorded || partial {
		t.Fatalf("unknown-generation recorded=%v partial=%v causes=%v, want the gen-2 clear row untouched", recorded, partial, causes)
	}
}
