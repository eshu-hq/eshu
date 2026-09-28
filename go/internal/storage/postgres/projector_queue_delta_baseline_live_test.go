// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const fenceProofScope = "scope-fence"

// fenceGen is one seeded generation. minutesAgo orders ingested_at; activated
// sets activated_at.
type fenceGen struct {
	id, commit, status, baseline string
	isDelta, activated           bool
	minutesAgo                   int
}

// provisionFenceProof seeds one scope, its generations, and a running work row
// owned by proof-worker (attempt 1) for each id in claimed.
func provisionFenceProof(t *testing.T, dsn, pointer string, gens []fenceGen, claimed ...string) *sql.DB {
	t.Helper()
	database := openLivenessProofDB(t, dsn)
	pointerSQL := "NULL"
	if pointer != "" {
		pointerSQL = "'" + pointer + "'"
	}
	var seed strings.Builder
	fmt.Fprintf(&seed, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key,
    collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ('%s', 'repository', 'github', 'proof/fence', 'git', 'proof/fence', now(), now(), 'active', %s);
`, fenceProofScope, pointerSQL)
	for _, g := range gens {
		baseline, activated := "NULL", "NULL"
		if g.baseline != "" {
			baseline = "'" + g.baseline + "'"
		}
		if g.activated {
			activated = fmt.Sprintf("now() - interval '%d minutes'", g.minutesAgo)
		}
		fmt.Fprintf(&seed, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at,
    ingested_at, status, activated_at, source_commit_sha, is_delta, delta_baseline_commit_sha)
VALUES ('%[1]s', '%[2]s', 'snapshot', now() - interval '%[3]d minutes', now() - interval '%[3]d minutes',
    '%[4]s', %[5]s, '%[6]s', %[7]t, %[8]s);
`, g.id, fenceProofScope, g.minutesAgo, g.status, activated, g.commit, g.isDelta, baseline)
	}
	for _, id := range claimed {
		fmt.Fprintf(&seed, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain,
    status, attempt_count, lease_owner, claim_until, visible_at, payload, created_at, updated_at)
VALUES ('projector_%[1]s', '%[2]s', '%[1]s', 'projector', 'source_local', 'running', 1, 'proof-worker',
    now() + interval '5 minutes', now(), '{}'::jsonb, now(), now());
`, id, fenceProofScope)
	}
	provisionLivenessSchema(t, database, seed.String())
	return database
}

func fenceWork(generationID string) projector.ScopeGenerationWork {
	return projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: fenceProofScope},
		Generation:   scope.ScopeGeneration{GenerationID: generationID},
		AttemptCount: 1,
	}
}

// fenceState is the durable outcome a refusal row asserts.
type fenceState struct {
	work, class, target, pointer, active string
	details                              map[string]string
}

func readFenceState(t *testing.T, database *sql.DB, target string) fenceState {
	t.Helper()
	var state fenceState
	var details sql.NullString
	if err := database.QueryRowContext(context.Background(), `
SELECT
    (SELECT status FROM fact_work_items WHERE generation_id = $1),
    (SELECT COALESCE(failure_class, '') FROM fact_work_items WHERE generation_id = $1),
    (SELECT failure_details FROM fact_work_items WHERE generation_id = $1),
    (SELECT status FROM scope_generations WHERE generation_id = $1),
    (SELECT COALESCE(active_generation_id, '') FROM ingestion_scopes WHERE scope_id = $2),
    COALESCE((SELECT string_agg(generation_id, ',') FROM scope_generations WHERE scope_id = $2 AND status = 'active'), '')`,
		target, fenceProofScope,
	).Scan(&state.work, &state.class, &details, &state.target, &state.pointer, &state.active); err != nil {
		t.Fatalf("read fence state: %v", err)
	}
	if details.Valid {
		if err := json.Unmarshal([]byte(details.String), &state.details); err != nil {
			t.Fatalf("decode failure_details %q: %v", details.String, err)
		}
	}
	return state
}

// assertFenceRefused checks the refusal contract: work and generation
// superseded, the active generation and scope pointer unchanged, the phase's
// class, and failure_details naming both commits.
func assertFenceRefused(t *testing.T, got fenceState, phase, pointer, baseline, activeCommit string) {
	t.Helper()
	class := projector.DeltaBaselineMismatchClass
	if phase == projector.DeltaBaselinePhaseAck {
		class = projector.DeltaBaselineMismatchAfterProjectionClass
	}
	if got.work != "superseded" || got.target != "superseded" || got.class != class ||
		got.pointer != pointer || got.active != pointer {
		t.Fatalf("state = %+v; want work+target superseded, class %s, pointer and active %q", got, class, pointer)
	}
	want := map[string]string{
		"scope_id": fenceProofScope, "delta_baseline_commit_sha": baseline,
		"active_commit_sha": activeCommit, "active_generation_id": pointer, "phase": phase,
	}
	for key, value := range want {
		if got.details[key] != value {
			t.Fatalf("failure_details[%s] = %q, want %q (details %v)", key, got.details[key], value, got.details)
		}
	}
}

func proofDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}
	return dsn
}

var (
	genActiveA = fenceGen{id: "gen-a", commit: "A", status: "active", activated: true, minutesAgo: 60}
	genActiveB = fenceGen{id: "gen-b", commit: "B", status: "active", activated: true, minutesAgo: 30}
)

func pendingDelta(baseline string) fenceGen {
	return fenceGen{id: "gen-d", commit: "D", status: "pending", isDelta: true, baseline: baseline, minutesAgo: 5}
}

// TestProjectorDeltaBaselineAckMatrix is the #7319 decision table against
// real Postgres, one row per case, through the production Ack.
func TestProjectorDeltaBaselineAckMatrix(t *testing.T) {
	dsn := proofDSN(t)
	type row struct {
		name, pointer string
		gens          []fenceGen
		wantActive    bool   // the delta activates
		outcome       string // counter outcome, "" for none
		refusedActive string // active commit named in a refusal
	}
	fullD := fenceGen{id: "gen-d", commit: "D", status: "pending", minutesAgo: 5}
	legacy := fenceGen{id: "gen-d", commit: "D", status: "pending", isDelta: true, minutesAgo: 5}
	activeD := fenceGen{id: "gen-d", commit: "D", status: "active", isDelta: true, baseline: "A", activated: true, minutesAgo: 5}
	for _, tc := range []row{
		{name: "baseline matches", pointer: "gen-a", gens: []fenceGen{genActiveA, pendingDelta("A")}, wantActive: true, outcome: "matched"},
		{name: "active differs", pointer: "gen-b", gens: []fenceGen{genActiveB, pendingDelta("A")}, outcome: "refused_active_differs", refusedActive: "B"},
		{
			name: "no active", gens: []fenceGen{{id: "gen-a", commit: "A", status: "superseded", activated: true, minutesAgo: 60}, pendingDelta("A")},
			outcome: "refused_no_active", refusedActive: "none",
		},
		{
			name: "failed target with no active", gens: []fenceGen{{id: "gen-d", commit: "D", status: "failed", isDelta: true, baseline: "A", minutesAgo: 5}},
			outcome: "refused_no_active", refusedActive: "none",
		},
		{name: "target already active", pointer: "gen-d", gens: []fenceGen{activeD}, wantActive: true, outcome: "already_active"},
		{name: "legacy delta without baseline", pointer: "gen-b", gens: []fenceGen{genActiveB, legacy}, wantActive: true, outcome: "unfenced"},
		{name: "full generation", pointer: "gen-b", gens: []fenceGen{genActiveB, fullD}, wantActive: true},
		{
			name: "full at the same commit activated in between", pointer: "gen-f",
			gens: []fenceGen{{id: "gen-f", commit: "A", status: "active", activated: true, minutesAgo: 20}, pendingDelta("A")}, wantActive: true, outcome: "matched",
		},
		{name: "active back at A by SHA", pointer: "gen-a2", gens: []fenceGen{
			{id: "gen-a1", commit: "A", status: "superseded", activated: true, minutesAgo: 90},
			{id: "gen-b", commit: "B", status: "superseded", activated: true, minutesAgo: 60},
			{id: "gen-a2", commit: "A", status: "active", activated: true, minutesAgo: 30},
			pendingDelta("A"),
		}, wantActive: true, outcome: "matched"},
		{name: "baseline is a never-activated commit", pointer: "gen-a", gens: []fenceGen{
			genActiveA, {id: "gen-x", commit: "X", status: "superseded", minutesAgo: 40}, pendingDelta("X"),
		}, outcome: "refused_active_differs", refusedActive: "A"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := provisionFenceProof(t, dsn, tc.pointer, tc.gens, "gen-d")
			queue := NewProjectorQueue(SQLDB{DB: database}, "proof-worker", time.Minute)
			instruments, reader := newEnqueueInstruments(t)
			queue.Instruments = instruments
			err := queue.Ack(context.Background(), fenceWork("gen-d"), runtime.Result{})
			got := readFenceState(t, database, "gen-d")
			if tc.wantActive {
				if err != nil || got.target != "active" || got.pointer != "gen-d" || got.active != "gen-d" || got.work != "succeeded" {
					t.Fatalf("Ack = %v, state %+v; want gen-d active and published", err, got)
				}
			} else {
				if !projector.IsDeltaBaselineRefusal(err) {
					t.Fatalf("Ack = %v, want a delta-baseline refusal", err)
				}
				assertFenceRefused(t, got, projector.DeltaBaselinePhaseAck, tc.pointer, tc.gens[len(tc.gens)-1].baseline, tc.refusedActive)
				// A duplicate Ack after the refusal is a lost claim and moves nothing.
				if dup := queue.Ack(context.Background(), fenceWork("gen-d"), runtime.Result{}); !isClaimRejected(dup) {
					t.Fatalf("duplicate Ack = %v, want ErrProjectorClaimRejected", dup)
				}
				if again := readFenceState(t, database, "gen-d"); again.work != got.work || again.target != got.target || again.pointer != got.pointer {
					t.Fatalf("duplicate Ack moved state: %+v -> %+v", got, again)
				}
			}
			assertFenceCounter(t, reader, projector.DeltaBaselinePhaseAck, tc.outcome)
		})
	}
}

// TestProjectorDeltaBaselineChain covers D1 then D2: D2 was diffed from D1's
// commit, which is only a valid baseline after D1 activated.
func TestProjectorDeltaBaselineChain(t *testing.T) {
	dsn := proofDSN(t)
	database := provisionFenceProof(t, dsn, "gen-a", []fenceGen{
		genActiveA,
		{id: "gen-d1", commit: "D1", status: "pending", isDelta: true, baseline: "A", minutesAgo: 10},
		{id: "gen-d2", commit: "D2", status: "pending", isDelta: true, baseline: "D1", minutesAgo: 5},
	}, "gen-d1", "gen-d2")
	queue := NewProjectorQueue(SQLDB{DB: database}, "proof-worker", time.Minute)
	// Before D1 activates, D2's baseline D1 is not the active commit.
	if state, err := queue.ReadDeltaBaseline(context.Background(), fenceWork("gen-d2")); err != nil ||
		projector.DecideDeltaBaseline(state) != projector.DeltaBaselineRefusedActiveDiffers {
		t.Fatalf("D2 before D1 = %+v, %v; want refused_active_differs", state, err)
	}
	for _, id := range []string{"gen-d1", "gen-d2"} {
		if err := queue.Ack(context.Background(), fenceWork(id), runtime.Result{}); err != nil {
			t.Fatalf("Ack(%s) = %v, want nil", id, err)
		}
	}
	if got := readFenceState(t, database, "gen-d2"); got.target != "active" || got.pointer != "gen-d2" {
		t.Fatalf("chain end state = %+v, want gen-d2 active", got)
	}
}

func isClaimRejected(err error) bool {
	return err != nil && strings.Contains(err.Error(), ErrProjectorClaimRejected.Error())
}

// markHookDB wraps the pool and runs before() the first time the refusal
// mark statement is issued, to inject a lost lease or a failed statement
// between Ack's rollback and its mark.
type markHookDB struct {
	SQLDB
	before func() error
	fired  bool
}

func (d *markHookDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if query == markProjectorDeltaBaselineRefusedQuery && !d.fired {
		d.fired = true
		if err := d.before(); err != nil {
			return nil, err
		}
	}
	return d.SQLDB.QueryContext(ctx, query, args...)
}
