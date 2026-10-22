// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8750 disposable PostgreSQL 18 proof that the destructive-successor guard sizes the prior on collector rows only (reducer_ rows excluded) and sizes a git delta on its file-level union against the base full generation; reuses the #7978 bootstrapped proof schema; wall time is logged, not gated
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
	envelopes := make([]facts.Envelope, 0, len(files)+extraCollectorFacts+1)
	if generation.IsDelta {
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
	stream := make(chan facts.Envelope, len(envelopes))
	for _, envelope := range envelopes {
		stream <- envelope
	}
	close(stream)
	if err := p.store.CommitScopeGeneration(context.Background(), p.scope, generation, stream); err != nil {
		p.t.Fatalf("commit %s: %v", generation.GenerationID, err)
	}
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
