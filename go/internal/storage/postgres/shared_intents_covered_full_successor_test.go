// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
)

// The shared intent store opts into the emitted-full-successor drain (#7165).
var _ worker.EmittedFullSuccessorReader = (*SharedIntentStore)(nil)

// TestCoveredByEmittedFullSuccessorIDsSQLShape locks the bounded lookup: one
// statement over the primary key, terminal status only, a newer full
// generation in (ingested_at, generation_id) order, and a domain-scoped
// emission proof. It must key on the terminal 'superseded' status and never
// on active_generation_id, which would race with activation and drop a
// pending generation's live intents. The domain predicate is load-bearing:
// acceptance units are repository ids in both lanes, so an unscoped emission
// check would let one lane's emission falsely cover the other's.
func TestCoveredByEmittedFullSuccessorIDsSQLShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"FROM scope_generations",
		"generation_id = ANY($1",
		"status = 'superseded'",
		"is_delta = false",
		"(f.ingested_at, f.generation_id) > (g.ingested_at, g.generation_id)",
		"FROM shared_projection_intents",
		"projection_domain = $2",
		"NOT EXISTS",
		"FROM fact_work_items",
		"FROM graph_projection_phase_repair_queue",
	} {
		if !strings.Contains(coveredByEmittedFullSuccessorSQL, want) {
			t.Fatalf("coveredByEmittedFullSuccessorSQL missing %q:\n%s", want, coveredByEmittedFullSuccessorSQL)
		}
	}
	if strings.Contains(coveredByEmittedFullSuccessorSQL, "active_generation_id") {
		t.Fatalf("lookup must not key on active_generation_id:\n%s", coveredByEmittedFullSuccessorSQL)
	}
}

func TestCoveredByEmittedFullSuccessorIDsOneRoundTripAndResultSet(t *testing.T) {
	t.Parallel()

	database := &supersededLookupDB{ids: []string{"gen-old"}}
	store := NewSharedIntentStore(database)

	got, err := store.CoveredByEmittedFullSuccessorIDs(context.Background(), "code_calls", []string{"gen-old", "gen-new"})
	if err != nil {
		t.Fatalf("CoveredByEmittedFullSuccessorIDs() error = %v", err)
	}
	if database.queries != 1 {
		t.Fatalf("queries = %d, want exactly 1 round trip", database.queries)
	}
	if _, ok := got["gen-old"]; !ok || len(got) != 1 {
		t.Fatalf("result = %v, want only gen-old", got)
	}
	if arg, _ := database.args[0].([]string); !slices.Equal(arg, []string{"gen-old", "gen-new"}) {
		t.Fatalf("args[0] = %v, want the id slice", database.args[0])
	}
	if arg, _ := database.args[1].(string); arg != "code_calls" {
		t.Fatalf("args[1] = %v, want the domain", database.args[1])
	}
}

func TestCoveredByEmittedFullSuccessorIDsEmptyInputSkipsQuery(t *testing.T) {
	t.Parallel()

	database := &supersededLookupDB{}
	got, err := NewSharedIntentStore(database).CoveredByEmittedFullSuccessorIDs(context.Background(), "code_calls", nil)
	if err != nil || len(got) != 0 || database.queries != 0 {
		t.Fatalf("got=%v err=%v queries=%d, want empty/nil/0", got, err, database.queries)
	}
}

func TestCoveredByEmittedFullSuccessorIDsPropagatesQueryError(t *testing.T) {
	t.Parallel()

	boom := errors.New("connection reset")
	_, err := NewSharedIntentStore(&supersededLookupDB{err: boom}).
		CoveredByEmittedFullSuccessorIDs(context.Background(), "code_calls", []string{"gen-a"})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want wrapped %v", err, boom)
	}
}

// TestCoveredByEmittedFullSuccessorShapesAgainstPostgres proves the #7165
// drain predicate on real data: the issue's four (full/delta) shapes plus the
// guard cases. A superseded generation is covered only when a newer full
// generation in the same scope emitted the lane's domain and no producer of
// the superseded generation is in flight. It bootstraps its own schema, so a
// bare disposable Postgres is enough. Set
// ESHU_SUPERSEDED_GENERATION_PROOF_DSN to run it; skipped otherwise. The
// reducer contention gate sets that DSN and selects this test by name.
func TestCoveredByEmittedFullSuccessorShapesAgainstPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	conn := openSupersededProofSchema(t, ctx, "eshu_7165_covered")
	store := NewSharedIntentStore(SQLDB{DB: conn})

	type generation struct {
		id     string
		delta  bool
		status string
	}
	type seed struct {
		name        string
		generations []generation
		// emit[row] lists "gen-index:domain[:completed]" emissions.
		emit []string
		// inFlightStage seeds a live producer on generations[0] when set.
		inFlightStage string
		// repair seeds a phase-repair row on generations[0] when true.
		repair bool
		// wantCovered lists the generation indexes covered for code_calls.
		wantCovered []int
		// wantCoveredRepo lists the generation indexes covered for
		// repo_dependency; nil means same as wantCovered.
		wantCoveredRepo []int
		sameAsCalls     bool
	}
	seeds := []seed{
		{
			name:        "full superseded, delta active keeps G1",
			generations: []generation{{id: "g1"}, {id: "g2", delta: true, status: "active"}},
			emit:        []string{"0:code_calls", "1:code_calls"},
		},
		{
			name:        "full, full drains G1",
			generations: []generation{{id: "g1"}, {id: "g2", status: "active"}},
			emit:        []string{"0:code_calls", "1:code_calls"},
			wantCovered: []int{0},
			sameAsCalls: true,
		},
		{
			name:        "full, delta superseded, full active drains G1 and G2",
			generations: []generation{{id: "g1"}, {id: "g2", delta: true}, {id: "g3", status: "active"}},
			emit:        []string{"0:code_calls", "1:code_calls", "2:code_calls"},
			wantCovered: []int{0, 1},
			sameAsCalls: true,
		},
		{
			name:        "full, full, delta active drains G0 and keeps G1",
			generations: []generation{{id: "g0"}, {id: "g1"}, {id: "g2", delta: true, status: "active"}},
			emit:        []string{"0:code_calls", "1:code_calls", "2:code_calls"},
			wantCovered: []int{0},
			sameAsCalls: true,
		},
		{
			name:        "unemitted full successor keeps G1",
			generations: []generation{{id: "g1"}, {id: "g2", status: "active"}},
			emit:        []string{"0:code_calls"},
		},
		{
			name:            "other-domain emission covers only its own lane",
			generations:     []generation{{id: "g1"}, {id: "g2", status: "active"}},
			emit:            []string{"0:code_calls", "0:repo_dependency", "1:repo_dependency"},
			wantCoveredRepo: []int{0},
		},
		{
			name:          "in-flight reducer producer defers the drain",
			generations:   []generation{{id: "g1"}, {id: "g2", status: "active"}},
			emit:          []string{"0:code_calls", "1:code_calls"},
			inFlightStage: "reducer",
		},
		{
			name:        "live phase-repair row defers the drain",
			generations: []generation{{id: "g1"}, {id: "g2", status: "active"}},
			emit:        []string{"0:code_calls", "1:code_calls"},
			repair:      true,
		},
		{
			name:        "completed successor rows still cover",
			generations: []generation{{id: "g1"}, {id: "g2", status: "active"}},
			emit:        []string{"0:code_calls", "1:code_calls:completed"},
			wantCovered: []int{0},
			sameAsCalls: true,
		},
		{
			name:        "failed successor that emitted still covers",
			generations: []generation{{id: "g1"}, {id: "g2", status: "failed"}},
			emit:        []string{"0:code_calls", "1:code_calls"},
			wantCovered: []int{0},
			sameAsCalls: true,
		},
		{
			name:        "completed successor still covers",
			generations: []generation{{id: "g1"}, {id: "g2", status: "completed"}},
			emit:        []string{"0:code_calls", "1:code_calls"},
			wantCovered: []int{0},
			sameAsCalls: true,
		},
	}

	now := time.Now().UTC()
	for i, s := range seeds {
		scopeID := fmt.Sprintf("repository:7165-covered-%d", i)
		if _, err := conn.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active')`, scopeID, now); err != nil {
			t.Fatalf("%s: seed scope: %v", s.name, err)
		}
		genIDs := make([]string, len(s.generations))
		for j, g := range s.generations {
			genIDs[j] = fmt.Sprintf("generation:7165-covered-%d-%s", i, g.id)
			status := g.status
			if status == "" {
				status = "superseded"
			}
			at := now.Add(time.Duration(j) * time.Second)
			if _, err := conn.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at, status)
VALUES ($1, $2, 'snapshot', $3, $4, $4, $5)`, genIDs[j], scopeID, g.delta, at, status); err != nil {
				t.Fatalf("%s: seed generation: %v", s.name, err)
			}
		}
		emissions := s.emit
		if s.sameAsCalls {
			// Mirror every code_calls emission into repo_dependency so the
			// same coverage holds in both lanes.
			for _, e := range s.emit {
				parts := strings.Split(e, ":")
				if parts[1] == "code_calls" {
					parts[1] = "repo_dependency"
					emissions = append(emissions, strings.Join(parts, ":"))
				}
			}
		}
		for k, e := range emissions {
			parts := strings.Split(e, ":")
			var genIdx int
			if _, err := fmt.Sscanf(parts[0], "%d", &genIdx); err != nil {
				t.Fatalf("%s: bad emission %q: %v", s.name, e, err)
			}
			completed := len(parts) > 2 && parts[2] == "completed"
			var completedAt *time.Time
			done := now.Add(time.Hour)
			if completed {
				completedAt = &done
			}
			if _, err := conn.ExecContext(ctx, `
INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id, payload, created_at, completed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '{}'::jsonb, $9, $10)`,
				fmt.Sprintf("intent:7165-covered-%d-%d", i, k), parts[1],
				fmt.Sprintf("unit-%d", genIdx), scopeID, "repo-7165", "repo-7165",
				fmt.Sprintf("run-%d", genIdx), genIDs[genIdx], now, completedAt); err != nil {
				t.Fatalf("%s: seed intent: %v", s.name, err)
			}
		}
		if s.inFlightStage != "" {
			if _, err := conn.ExecContext(ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, claim_until, payload, created_at, updated_at)
VALUES ($1, $2, $3, $4, 'workload_materialization', 'intent', $1, 'claimed', 1, $5, '{}'::jsonb, $6, $6)`,
				fmt.Sprintf("work:7165-covered-%d", i), scopeID, genIDs[0],
				s.inFlightStage, now.Add(10*time.Minute), now); err != nil {
				t.Fatalf("%s: seed work item: %v", s.name, err)
			}
		}
		if s.repair {
			if _, err := conn.ExecContext(ctx, `
INSERT INTO graph_projection_phase_repair_queue
    (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase,
     committed_at, enqueued_at, next_attempt_at)
VALUES ($1, 'unit', $2, $2, 'service_uid', 'workload_materialization', $3, $3, $4)`,
				scopeID, genIDs[0], now, now.Add(5*time.Minute)); err != nil {
				t.Fatalf("%s: seed repair row: %v", s.name, err)
			}
		}

		for _, domain := range []string{"code_calls", "repo_dependency"} {
			want := s.wantCoveredRepo
			if domain == "code_calls" || s.sameAsCalls {
				want = s.wantCovered
			}
			// The wrong-domain seed sets wantCoveredRepo only; every other
			// seed pins code_calls, and sameAsCalls mirrors it.
			if domain == "repo_dependency" && !s.sameAsCalls && s.wantCoveredRepo == nil && len(s.wantCovered) == 0 {
				continue
			}
			got, err := store.CoveredByEmittedFullSuccessorIDs(ctx, domain, genIDs)
			if err != nil {
				t.Fatalf("%s/%s: CoveredByEmittedFullSuccessorIDs() error = %v", s.name, domain, err)
			}
			wantSet := make(map[int]bool, len(want))
			for _, j := range want {
				wantSet[j] = true
			}
			for j, id := range genIDs {
				_, covered := got[id]
				if covered != wantSet[j] {
					t.Errorf("%s/%s: generation %d covered = %t, want %t (got %v)",
						s.name, domain, j, covered, wantSet[j], got)
				}
			}
		}
	}

	// An id that is not a scope generation stays absent.
	got, err := store.CoveredByEmittedFullSuccessorIDs(ctx, "code_calls",
		[]string{"relationship-generation-not-a-scope-generation"})
	if err != nil {
		t.Fatalf("non-generation lookup error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("non-generation lookup = %v, want empty", got)
	}
}
