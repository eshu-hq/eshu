// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestTargetedMaintenanceInterleavingsMatchWholePass extends the #7584
// write-side differential to the fixtures where state moves under the pass or a
// write fails: each acts at the same semantic point in both arms through a
// hookBeginner, so the arms stay comparable.
func TestTargetedMaintenanceInterleavingsMatchWholePass(t *testing.T) {
	t.Run("generation_advances_between_read_and_evidence_commit", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.prepass()
		p.quietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")
		outcome := p.run("generation_advances_between_read_and_evidence_commit", targetedDiffCase{
			owed:     owedPartitions("git:tgt", "tgt-2"),
			compared: partitionSet("git:tgt", "tgt-2"),
			// tgt-3 activates before the first batch: the batch guard skips
			// tgt, so no evidence and no phase; tgt-2's relationship and
			// correlation items are all below the replay floor in both
			// arms (#7637 extended the floor to the relationship
			// listings), so nothing reopens.
			outcomes: map[string]TargetedMaintenanceOutcomeKind{"git:tgt/tgt-2": TargetedMaintenanceRetry},
			hooks: func(_ string, database *sql.DB) *hookBeginner {
				return &hookBeginner{onBegin: func(n int) error {
					if n == 1 {
						return activateSuccessor(database, "tgt-3")
					}
					return nil
				}}
			},
		})
		assertNoBackwardPhase(t, outcome, "git:tgt", "tgt-2", "tgt-3")
	})

	t.Run("generation_advances_between_evidence_commit_and_phase", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.prepass()
		p.quietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")
		outcome := p.run("generation_advances_between_evidence_commit_and_phase", targetedDiffCase{
			owed:        owedPartitions("git:tgt", "tgt-2"),
			compared:    partitionSet("git:tgt", "tgt-2"),
			newEvidence: []string{"repo-tgt->repo-dep"},
			// tgt-2 is superseded mid-pass: its relationship items stay
			// succeeded under the #7637 replay floor, so nothing reopens.
			outcomes: map[string]TargetedMaintenanceOutcomeKind{"git:tgt/tgt-2": TargetedMaintenanceRetry},
			hooks: func(_ string, database *sql.DB) *hookBeginner {
				fired := false
				return &hookBeginner{onQuery: func(query string, args []any) error {
					if fired || query != activeScopeGenerationQuery || len(args) == 0 || args[0] != "git:tgt" {
						return nil
					}
					fired = true
					return activateSuccessor(database, "tgt-3")
				}}
			},
		})
		assertNoBackwardPhase(t, outcome, "git:tgt", "tgt-2", "tgt-3")
	})

	t.Run("successor_activation", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.prepass()
		p.quietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")
		p.exec(`UPDATE scope_generations SET ingested_at = ingested_at - interval '30 minutes' WHERE generation_id = 'tgt-2'`)
		p.quietGeneration("git:tgt", "tgt-3", "repo-tgt", "payments-service", "orders-api")
		// The obligation for the superseded tgt-2 must do nothing at all.
		outcome := p.run("successor_activation_superseded_owed", targetedDiffCase{
			owed:     owedPartitions("git:tgt", "tgt-2"),
			compared: map[scopeGenerationPartition]struct{}{},
			outcomes: map[string]TargetedMaintenanceOutcomeKind{"git:tgt/tgt-2": TargetedMaintenanceNotActive},
		})
		if _, ok := outcome.whole[phaseKey("git:tgt", "tgt-3")]; !ok {
			t.Fatal("whole arm did not publish the successor tgt-3")
		}
	})

	t.Run("successor_activation_owed", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.prepass()
		p.quietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")
		p.exec(`UPDATE scope_generations SET ingested_at = ingested_at - interval '30 minutes' WHERE generation_id = 'tgt-2'`)
		p.quietGeneration("git:tgt", "tgt-3", "repo-tgt", "payments-service", "orders-api")
		p.run("successor_activation_owed", targetedDiffCase{
			owed:        owedPartitions("git:tgt", "tgt-3"),
			compared:    partitionSet("git:tgt", "tgt-3"),
			newEvidence: []string{"repo-tgt->repo-dep"},
			published:   partitionSet("git:tgt", "tgt-3"),
			reopened:    workIDs("tgt-3"),
		})
	})

	t.Run("failed_sibling_batch_then_retry", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.gitRepo("git:mono", "mono-1", "repo-m1", "mono-alpha")
		p.repo("git:mono", "mono-1", "repo-m2", "mono-beta")
		p.workItems("git:mono", "mono-1")
		p.prepass()
		seedTargetedMonoGeneration(p)
		// repo-m2's batch fails after repo-m1's batch committed: no phase may
		// be published for the shared partition and nothing is reopened.
		p.run("failed_sibling_batch", targetedDiffCase{
			owed:        owedPartitions("git:mono", "mono-2"),
			compared:    partitionSet("git:mono", "mono-2"),
			newEvidence: []string{"repo-m1->repo-dep"},
			batchSize:   1,
			wantErr:     true,
			outcomes:    map[string]TargetedMaintenanceOutcomeKind{"git:mono/mono-2": TargetedMaintenanceRetry},
			hooks: func(_ string, _ *sql.DB) *hookBeginner {
				return &hookBeginner{onExec: func(query string, args []any) error {
					if strings.HasPrefix(strings.TrimSpace(query), "INSERT INTO relationship_evidence_facts") &&
						argsContain(args, "repo-m2") {
						return errors.New("injected sibling batch failure")
					}
					return nil
				}}
			},
		})
		// The retry is a clean pass: the phase lands only now, after both
		// sibling evidence commits.
		p.run("failed_sibling_batch_retry", targetedDiffCase{
			owed:        owedPartitions("git:mono", "mono-2"),
			compared:    partitionSet("git:mono", "mono-2"),
			newEvidence: []string{"repo-m2->repo-dep"},
			published:   partitionSet("git:mono", "mono-2"),
			reopened:    workIDs("mono-2"),
			batchSize:   1,
		})
	})

	t.Run("unprocessed_inbound_source_is_promoted", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.gitRepo("git:in", "in-1", "repo-in", "billing-ui")
		p.terraformRef("in-1-ref", "git:in", "in-1", "repo-in", "in.tf", "payments-service")
		p.workItems("git:in", "in-1")
		p.prepass()
		p.quietGeneration("git:in", "in-2", "repo-in", "billing-ui", "payments-service")
		p.quietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")
		outcome := p.run("unprocessed_inbound_source_is_promoted", targetedDiffCase{
			owed:        owedPartitions("git:tgt", "tgt-2"),
			compared:    partitionSet("git:tgt", "tgt-2", "git:in", "in-2"),
			newEvidence: []string{"repo-in->repo-tgt", "repo-tgt->repo-dep"},
			published:   partitionSet("git:tgt", "tgt-2", "git:in", "in-2"),
			reopened:    concatIDs(workIDs("tgt-2"), workIDs("in-2")),
		})
		if want := owedPartitions("git:in", "in-2"); !reflect.DeepEqual(outcome.result.Promoted, want) {
			t.Fatalf("promoted = %v, want %v", outcome.result.Promoted, want)
		}
	})

	t.Run("catalog_change_is_refused", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.prepass()
		p.gitRepo("git:new", "new-1", "repo-new", "inventory-svc")
		p.terraformRef("new-1-ref", "git:new", "new-1", "repo-new", "main.tf", "orders-api")
		p.workItems("git:new", "new-1")
		before := p.capture(p.targeted)
		store := targetedDiffStore(p.targeted, targetedDiffArmsAt)
		result, err := store.RunDeferredRelationshipMaintenanceForPartitions(p.ctx, nil, nil, owedPartitions("git:new", "new-1"))
		if !errors.Is(err, ErrTargetedMaintenanceCatalogChanged) {
			t.Fatalf("partition-scoped pass after onboarding error = %v, want ErrTargetedMaintenanceCatalogChanged", err)
		}
		if want := []TargetedMaintenanceOutcome{{Partition: OwedPartition{ScopeID: "git:new", GenerationID: "new-1"}, Kind: TargetedMaintenanceRetry}}; !reflect.DeepEqual(result.Outcomes, want) {
			t.Fatalf("refused pass outcomes = %v, want %v", result.Outcomes, want)
		}
		if changed := diffStates(before, before, p.capture(p.targeted), nil).targetedChangedOutside; len(changed) > 0 {
			t.Fatalf("refused pass changed rows: %v", changed)
		}
	})
}

// activateSuccessor commits a successor generation of git:tgt directly on
// database, as an ingestion commit racing the pass would.
func activateSuccessor(database *sql.DB, generationID string) error {
	at := targetedDiffBase.Add(100 * time.Minute)
	statements := []struct {
		query string
		args  []any
	}{
		{`UPDATE scope_generations SET status = 'superseded', superseded_at = $1 WHERE scope_id = 'git:tgt' AND status = 'active'`, []any{at}},
		{`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ($1, 'git:tgt', 'poll', $2, $2, 'active', $2)`, []any{generationID, at}},
		{
			`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, 'git:tgt', $2, 'repository', $1, 'git', $1, $3, $3, '{"repo_id":"repo-tgt","name":"payments-service"}'::jsonb)`,
			[]any{"repo-" + generationID + "-repo-tgt", generationID, targetedDiffBase},
		},
		{`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = 'git:tgt'`, []any{generationID}},
	}
	for _, statement := range statements {
		if _, err := database.Exec(statement.query, statement.args...); err != nil {
			return fmt.Errorf("activate successor %s: %w", generationID, err)
		}
	}
	return nil
}

// assertNoBackwardPhase fails when either arm holds a backward_evidence phase
// for any of the given generations of scopeID.
func assertNoBackwardPhase(t *testing.T, outcome targetedDiffOutcome, scopeID string, generationIDs ...string) {
	t.Helper()
	for _, generationID := range generationIDs {
		for arm, state := range map[string]targetedDiffState{"whole": outcome.whole, "targeted": outcome.targeted} {
			if _, ok := state[phaseKey(scopeID, generationID)]; ok {
				t.Fatalf("%s arm published a phase for superseded or skipped generation %s", arm, generationID)
			}
		}
	}
}

// argsContain reports whether any argument equals value.
func argsContain(args []any, value string) bool {
	for _, arg := range args {
		if text, ok := arg.(string); ok && text == value {
			return true
		}
	}
	return false
}
