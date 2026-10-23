// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8750 disposable PostgreSQL 18 proof that the destructive-successor guard sizes the prior on collector rows only (reducer_ rows excluded) and sizes a git delta on its file-level union against the base full generation, and sizes the prior as the larger of the active generation and a live pending candidate (review F1); reuses the #7978 bootstrapped proof schema; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// guardSizingFile is one collector file fact for the #8750 proofs.
type guardSizingFile struct {
	path      string
	tombstone bool
}

// commitGuardSizing streams files (one file fact each) plus extra non-file
// collector facts through the production IngestionStore. A delta generation
// also streams its delta_generation repository fact, as the git collector
// does, naming changed and deleted paths.
func (p *reprojectProof) commitGuardSizing(
	generation scope.ScopeGeneration,
	files []guardSizingFile,
	extraCollectorFacts int,
	changed, deleted []string,
) {
	p.t.Helper()
	p.commitGuardSizingStream(generation, files, extraCollectorFacts, changed, deleted, generation.IsDelta)
}

// commitGuardSizingWithoutRepositoryFact streams a delta's facts with no
// delta_generation repository fact.
func (p *reprojectProof) commitGuardSizingWithoutRepositoryFact(
	generation scope.ScopeGeneration,
	files []guardSizingFile,
	extraCollectorFacts int,
) {
	p.t.Helper()
	p.commitGuardSizingStream(generation, files, extraCollectorFacts, nil, nil, false)
}

func (p *reprojectProof) commitGuardSizingStream(
	generation scope.ScopeGeneration,
	files []guardSizingFile,
	extraCollectorFacts int,
	changed, deleted []string,
	withRepositoryFact bool,
) {
	p.t.Helper()
	envelopes := guardSizingEnvelopes(generation, files, extraCollectorFacts, changed, deleted, withRepositoryFact)
	stream := make(chan facts.Envelope, len(envelopes))
	for _, envelope := range envelopes {
		stream <- envelope
	}
	close(stream)
	if err := p.store.CommitScopeGeneration(context.Background(), p.scope, generation, stream); err != nil {
		p.t.Fatalf("commit %s: %v", generation.GenerationID, err)
	}
}

// guardSizingEnvelopes builds the collector facts commitGuardSizing streams:
// an optional delta_generation repository fact, one file fact per file, and
// extraCollectorFacts content entities.
func guardSizingEnvelopes(
	generation scope.ScopeGeneration,
	files []guardSizingFile,
	extraCollectorFacts int,
	changed, deleted []string,
	withRepositoryFact bool,
) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, len(files)+extraCollectorFacts+1)
	if withRepositoryFact {
		envelopes = append(envelopes, facts.Envelope{
			FactID: generation.GenerationID + "-repository", ScopeID: generation.ScopeID,
			GenerationID: generation.GenerationID, FactKind: "repository",
			StableFactKey: "repository:proof/7978", ObservedAt: generation.ObservedAt,
			Payload: map[string]any{
				"repo_id": "proof/7978", "path": "/repos/proof", "delta_generation": true,
				"delta_relative_paths": toAnySlice(changed), "delta_deleted_relative_paths": toAnySlice(deleted),
			},
		})
	}
	for i, file := range files {
		envelopes = append(envelopes, facts.Envelope{
			FactID:        fmt.Sprintf("%s-file-%04d", generation.GenerationID, i),
			ScopeID:       generation.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      "file",
			StableFactKey: "file:proof/7978:" + file.path,
			ObservedAt:    generation.ObservedAt,
			IsTombstone:   file.tombstone,
			Payload:       map[string]any{"relative_path": file.path, "repo_id": "proof/7978"},
		})
	}
	for i := 0; i < extraCollectorFacts; i++ {
		envelopes = append(envelopes, facts.Envelope{
			FactID:        fmt.Sprintf("%s-entity-%04d", generation.GenerationID, i),
			ScopeID:       generation.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      "content_entity",
			StableFactKey: fmt.Sprintf("content_entity:proof/7978:e%04d", i),
			ObservedAt:    generation.ObservedAt,
			Payload:       map[string]any{"relative_path": fmt.Sprintf("f%04d.go", i)},
		})
	}
	return envelopes
}

// seedReducerRows writes reducer-derived rows into an activated generation,
// as reducer domains do after projection (search documents, drift findings).
func (p *reprojectProof) seedReducerRows(generationID string, count int) {
	p.t.Helper()
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("reducer_eshu_search_document:%s:%04d", generationID, i)
		if _, err := p.db.ExecContext(context.Background(), `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload
) VALUES ($1, $2, $3, 'reducer_eshu_search_document', $4, 'git', $4, now(), now(), '{}'::jsonb)`,
			fmt.Sprintf("%s-reducer-%04d", generationID, i), p.scope.ScopeID, generationID, key); err != nil {
			p.t.Fatalf("seed reducer row: %v", err)
		}
	}
}

// generationStored reports whether generationID has a scope_generations row.
func (p *reprojectProof) generationStored(generationID string) bool {
	p.t.Helper()
	var n int
	if err := p.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM scope_generations WHERE generation_id = $1`, generationID).Scan(&n); err != nil {
		p.t.Fatalf("read generation %s: %v", generationID, err)
	}
	return n == 1
}

func guardSizingFiles(prefix string, count int) []guardSizingFile {
	files := make([]guardSizingFile, 0, count)
	for i := 0; i < count; i++ {
		files = append(files, guardSizingFile{path: fmt.Sprintf("%s%04d.go", prefix, i)})
	}
	return files
}

func toAnySlice(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

// TestGuardStoresForcedFullOverReducerHeavyPriorLive is the #8750 like-for-like
// regression: the prior's raw row count includes reducer_ rows the reducer
// wrote into it after projection, which no collector stream ever carries. A
// forced full holding 0.74 of the prior's collector rows is ordinary drift and
// must store, even though it is under half of the raw total.
func TestGuardStoresForcedFullOverReducerHeavyPriorLive(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)

	// gen-a: 54 collector rows (27 files + 27 entities), then 46 reducer rows.
	p.commitGuardSizing(p.generation("gen-a", "hint-a", "sha-a", ""), guardSizingFiles("f", 27), 27, nil, nil)
	p.project()
	p.seedReducerRows("gen-a", 46)

	// gen-b: forced full of 40 collector rows (20 files + 20 entities).
	p.commitGuardSizing(p.generation("gen-b", "hint-b", "sha-b", "interval_elapsed"), guardSizingFiles("f", 20), 20, nil, nil)

	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != 0 {
		t.Fatalf("destructive_successor skips = %d, want 0: 40 of 54 collector rows is not a sharp shrink", got)
	}
	if !p.generationStored("gen-b") {
		t.Fatal("gen-b was not stored")
	}
}

// TestGuardRefusesSharplyShrunkenFullOverReducerHeavyPriorLive keeps the
// backstop sharp after the like-for-like change: a full holding under half of
// the prior's collector rows is still refused.
func TestGuardRefusesSharplyShrunkenFullOverReducerHeavyPriorLive(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)

	p.commitGuardSizing(p.generation("gen-a", "hint-a", "sha-a", ""), guardSizingFiles("f", 27), 27, nil, nil)
	p.project()
	p.seedReducerRows("gen-a", 46)

	// 26 collector rows: under half of 54.
	p.commitGuardSizing(p.generation("gen-b", "hint-b", "sha-b", "interval_elapsed"), guardSizingFiles("f", 13), 13, nil, nil)

	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != 1 {
		t.Fatalf("destructive_successor skips = %d, want 1", got)
	}
	if p.generationStored("gen-b") {
		t.Fatal("gen-b was stored; a sharp shrink of collector rows must be refused")
	}
}

// deltaGeneration builds a delta generation diffed from baseline.
func (p *reprojectProof) deltaGeneration(id, hint, sha, baseline string) scope.ScopeGeneration {
	generation := p.generation(id, hint, sha, "")
	generation.IsDelta = true
	generation.DeltaBaselineCommitSHA = baseline
	return generation
}

// seedLargeFullPrior commits and projects gen-a at sha-a with files source
// files and one entity each, then adds reducer rows, mirroring a projected
// repository whose reducer domains have run.
func (p *reprojectProof) seedLargeFullPrior(files, reducerRows int) {
	p.t.Helper()
	p.commitGuardSizing(p.generation("gen-a", "hint-a", "sha-a", ""), guardSizingFiles("f", files), files, nil, nil)
	if got := p.project(); len(got) != 1 || got[0] != "gen-a" {
		p.t.Fatalf("projected = %v, want [gen-a]", got)
	}
	p.seedReducerRows("gen-a", reducerRows)
}

func guardSizingPaths(prefix string, from, to int) []string {
	paths := make([]string, 0, to-from)
	for i := from; i < to; i++ {
		paths = append(paths, fmt.Sprintf("%s%04d.go", prefix, i))
	}
	return paths
}

func guardSizingTombstones(paths []string) []guardSizingFile {
	files := make([]guardSizingFile, 0, len(paths))
	for _, path := range paths {
		files = append(files, guardSizingFile{path: path, tombstone: true})
	}
	return files
}

func guardSizingLive(paths []string) []guardSizingFile {
	files := make([]guardSizingFile, 0, len(paths))
	for _, path := range paths {
		files = append(files, guardSizingFile{path: path})
	}
	return files
}

// TestGuardStoresSmallDeltaOverLargePriorLive is the #8750 delta regression:
// a webhook delta re-parsing 41 changed files carries only their facts, but
// its projected union keeps every other file of the base. Sized by its own
// stream it looked like a 90% shrink and was refused.
func TestGuardStoresSmallDeltaOverLargePriorLive(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	p.seedLargeFullPrior(200, 300)

	changed := guardSizingPaths("f", 0, 41)
	p.commitGuardSizing(p.deltaGeneration("gen-b", "hint-b", "sha-b", "sha-a"), guardSizingLive(changed), 41, changed, nil)

	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != 0 {
		t.Fatalf("destructive_successor skips = %d, want 0: a 41-file delta keeps the other 159 files", got)
	}
	if !p.generationStored("gen-b") {
		t.Fatal("gen-b was not stored")
	}
}

// TestGuardRefusesMassDeleteDeltaLive keeps the backstop sharp for deltas: a
// delta whose diff deletes 150 of the base's 200 indexed files would leave a
// 50-file union, a sharp shrink, so it is refused like the equivalent full.
func TestGuardRefusesMassDeleteDeltaLive(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	p.seedLargeFullPrior(200, 300)

	deleted := guardSizingPaths("f", 0, 150)
	p.commitGuardSizing(p.deltaGeneration("gen-b", "hint-b", "sha-b", "sha-a"), guardSizingTombstones(deleted), 0, deleted, deleted)

	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != 1 {
		t.Fatalf("destructive_successor skips = %d, want 1", got)
	}
	if p.generationStored("gen-b") {
		t.Fatal("gen-b was stored; deleting 150 of 200 indexed files must be refused")
	}
}

// TestGuardStoresRenameDeltaLive proves renames net to zero: git reports a
// rename as the old path deleted and the new path changed, so moving 120 of
// 200 files fails the zero-query lower bound and the exact probe stores it.
func TestGuardStoresRenameDeltaLive(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	p.seedLargeFullPrior(200, 300)

	oldPaths := guardSizingPaths("f", 0, 120)
	newPaths := guardSizingPaths("moved/f", 0, 120)
	files := append(guardSizingTombstones(oldPaths), guardSizingLive(newPaths)...)
	p.commitGuardSizing(p.deltaGeneration("gen-b", "hint-b", "sha-b", "sha-a"), files, 120,
		append(append([]string{}, oldPaths...), newPaths...), oldPaths)

	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != 0 {
		t.Fatalf("destructive_successor skips = %d, want 0: a rename keeps every file", got)
	}
	if !p.generationStored("gen-b") {
		t.Fatal("gen-b was not stored")
	}
}

// TestGuardStoresDeltaDeletingNeverIndexedPathsLive proves the deleted list
// is not taken at face value: git names every deleted path, including build
// output Eshu never indexed. Removing 150 such paths removes no base file.
func TestGuardStoresDeltaDeletingNeverIndexedPathsLive(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	p.seedLargeFullPrior(200, 300)

	deleted := guardSizingPaths("dist/bundle", 0, 150)
	p.commitGuardSizing(p.deltaGeneration("gen-b", "hint-b", "sha-b", "sha-a"), guardSizingTombstones(deleted), 0, deleted, deleted)

	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != 0 {
		t.Fatalf("destructive_successor skips = %d, want 0: no indexed file was deleted", got)
	}
	if !p.generationStored("gen-b") {
		t.Fatal("gen-b was not stored")
	}
}

// TestGuardStoresDeltaWithMissingBaseLive leaves a delta with no projected
// full generation at its baseline to the projector, which refuses it as
// base-missing and arms the reindex watermark for a forced full. A refusal
// here would arm nothing and loop on every webhook.
func TestGuardStoresDeltaWithMissingBaseLive(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	p.seedLargeFullPrior(200, 300)

	changed := guardSizingPaths("f", 0, 5)
	p.commitGuardSizing(p.deltaGeneration("gen-b", "hint-b", "sha-b", "sha-unknown"), guardSizingLive(changed), 5, changed, nil)

	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != 0 {
		t.Fatalf("destructive_successor skips = %d, want 0", got)
	}
	if !p.generationStored("gen-b") {
		t.Fatal("gen-b was not stored")
	}
}

// TestGuardSizesDeltaWithoutRepositoryFactAsFullLive fails closed when a
// generation claims to be a delta but streams no delta_generation repository
// fact: without the touched paths the union is unknowable, so the full rule
// applies and a small stream over a large prior is refused.
func TestGuardSizesDeltaWithoutRepositoryFactAsFullLive(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	p.seedLargeFullPrior(200, 300)

	generation := p.deltaGeneration("gen-b", "hint-b", "sha-b", "sha-a")
	p.commitGuardSizingWithoutRepositoryFact(generation, guardSizingLive(guardSizingPaths("f", 0, 41)), 41)

	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != 1 {
		t.Fatalf("destructive_successor skips = %d, want 1", got)
	}
	if p.generationStored("gen-b") {
		t.Fatal("gen-b was stored; an unverifiable delta must be sized as a full")
	}
}

// openChainedDeltaPendingWindow opens the review F1 window: gen-a (200
// files, 200 entities, 300 reducer rows) is projected, the delta gen-b over
// it is projected (active union 400 + 11 = 411 collector rows), and the
// chained delta gen-c (11 rows) is stored and pending. A second delta in a
// chain has no anchor (its baseline is the active delta's commit), so it
// stays pending until the projector refuses it.
func openChainedDeltaPendingWindow(t *testing.T) *reprojectProof {
	t.Helper()
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	p.seedLargeFullPrior(200, 300)
	first := guardSizingPaths("f", 0, 5)
	p.commitGuardSizing(p.deltaGeneration("gen-b", "hint-b", "sha-b", "sha-a"), guardSizingLive(first), 5, first, nil)
	if got := p.project(); len(got) != 1 || got[0] != "gen-b" {
		t.Fatalf("projected = %v, want [gen-b]", got)
	}
	second := guardSizingPaths("f", 5, 10)
	p.commitGuardSizing(p.deltaGeneration("gen-c", "hint-c", "sha-c", "sha-b"), guardSizingLive(second), 5, second, nil)
	if !p.generationStored("gen-c") {
		t.Fatal("chained delta gen-c was not stored; the window under test never opened")
	}
	return p
}

// requireGuardOutcome asserts whether generationID was stored and the
// running destructive-successor skip count.
func (p *reprojectProof) requireGuardOutcome(generationID string, stored bool, skips uint64) {
	p.t.Helper()
	if got := p.generationStored(generationID); got != stored {
		p.t.Fatalf("%s stored = %v, want %v", generationID, got, stored)
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != skips {
		p.t.Fatalf("destructive_successor skips = %d, want %d", got, skips)
	}
}

// TestGuardRefusesSharpShrinkFullWhileChainedDeltaPendingLive is review F1
// (L1): a full that arrives while a chained delta is pending must still be
// sized against the scope's projected truth, not the pending delta's few rows.
func TestGuardRefusesSharpShrinkFullWhileChainedDeltaPendingLive(t *testing.T) {
	p := openChainedDeltaPendingWindow(t)
	// 20 files and 20 entities: a tenth of the projected repository.
	p.commitGuardSizing(p.generation("gen-d", "hint-d", "sha-d", "interval_elapsed"), guardSizingFiles("f", 20), 20, nil, nil)
	p.requireGuardOutcome("gen-d", false, 1)
}

// TestGuardStoresHalfSizeFullWhileChainedDeltaPendingLive (L2) proves the
// prior is the larger side, not the sum: 208 rows hold half the active union
// (416 >= 411) but not half of active plus pending (416 < 422).
func TestGuardStoresHalfSizeFullWhileChainedDeltaPendingLive(t *testing.T) {
	p := openChainedDeltaPendingWindow(t)
	p.commitGuardSizing(p.generation("gen-d", "hint-d", "sha-d", "interval_elapsed"), guardSizingFiles("f", 104), 104, nil, nil)
	p.requireGuardOutcome("gen-d", true, 0)
}

// TestGuardSizesAgainstLargerPendingFullLive (L3): a pending full larger than
// the active counts, because the claim sweep lets a newer full supersede it
// unprojected. Active 200 rows, pending 800: 300 rows passes the active alone
// but is refused; 500 rows holds half the pending and stores.
func TestGuardSizesAgainstLargerPendingFullLive(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	p.seedLargeFullPrior(100, 0)
	p.commitGuardSizing(p.generation("gen-b", "hint-b", "sha-b", "interval_elapsed"), guardSizingFiles("f", 400), 400, nil, nil)
	p.requireGuardOutcome("gen-b", true, 0)

	p.commitGuardSizing(p.generation("gen-c", "hint-c", "sha-c", "interval_elapsed"), guardSizingFiles("f", 20), 20, nil, nil)
	p.requireGuardOutcome("gen-c", false, 1)
	p.commitGuardSizing(p.generation("gen-d", "hint-d", "sha-d", "interval_elapsed"), guardSizingFiles("f", 150), 150, nil, nil)
	p.requireGuardOutcome("gen-d", false, 2)
	p.commitGuardSizing(p.generation("gen-e", "hint-e", "sha-e", "interval_elapsed"), guardSizingFiles("f", 250), 250, nil, nil)
	p.requireGuardOutcome("gen-e", true, 2)
}

// TestGuardRefusesStepwiseShrinkThroughPendingFullLive (L4): active 400 rows,
// a pending full of 200 stores (exactly half), then a 120-row full passes the
// pending alone (240 >= 200) but would shrink the graph to 120 of 400: refused.
func TestGuardRefusesStepwiseShrinkThroughPendingFullLive(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	p.seedLargeFullPrior(200, 0)
	p.commitGuardSizing(p.generation("gen-b", "hint-b", "sha-b", "interval_elapsed"), guardSizingFiles("f", 100), 100, nil, nil)
	p.requireGuardOutcome("gen-b", true, 0)

	p.commitGuardSizing(p.generation("gen-c", "hint-c", "sha-c", "interval_elapsed"), guardSizingFiles("f", 60), 60, nil, nil)
	p.requireGuardOutcome("gen-c", false, 1)
}
