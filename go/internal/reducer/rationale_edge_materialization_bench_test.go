// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const rationaleHandlerBenchmarkEntityCount = 5_000

// BenchmarkRationaleEdgeMaterializationHandlerRepoScale measures the in-process
// cost newly activated by the unconditional collector follow-up: extraction and
// durable refresh/edge-intent construction. The loader and writer are
// intentionally in-memory, so these numbers do not claim Postgres or graph I/O.
//
// The Postgres side of that follow-up -- the per-generation
// ListFactsByKind([repository, content_entity]) load this handler issues -- is
// measured against a real store in
// docs/internal/evidence/6154-fact-records-keyset-pagination.md: on the
// 896-repository corpus the worst-case generation (241,726 facts sharing one
// observed_at) loaded in 84.12 s and was quadratic in generation size. That was
// root-caused and fixed in #6155 (commit e6453efd, keyset paging plus the
// statement split), taking the same load to 3.37 s.
//
// Only the LOAD is comparable across the two branches, and the numbers above
// are load numbers. Do not read them as a handler speedup: main has no
// rationale follow-up, so its handler returns "no repositories available for
// rationale materialization" without doing this work at all. The evidence doc
// reports the two handler totals (84.34 s here, 3.49 s on main) apart from the
// load table for exactly that reason and draws no speedup from them.
//
// Reverting the fix is caught at COMPILE time, not by the live test:
// facts_filtered_keyset_test.go and fact_records_keyset_index_live_test.go
// both reference listFactsByKindCursorQuery, which the fix introduced, so the
// storage/postgres test build fails. The live test cannot itself guard a
// revert credential-free -- it skips without ESHU_POSTGRES_TEST_DSN.
func BenchmarkRationaleEdgeMaterializationHandlerRepoScale(b *testing.B) {
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(previousLogger) })

	for _, test := range []struct {
		name                string
		positiveEvery       int
		wantCanonicalWrites int
	}{
		{name: "zero_rationale", wantCanonicalWrites: 1},
		{name: "positive_rationale", positiveEvery: 1, wantCanonicalWrites: 5_001},
	} {
		b.Run(test.name, func(b *testing.B) {
			envelopes := rationaleHandlerBenchmarkFacts(test.positiveEvery)
			loader := &rationaleBenchmarkFactLoader{envelopes: envelopes}
			writer := &rationaleBenchmarkIntentWriter{}
			handler := RationaleEdgeMaterializationHandler{
				FactLoader:   loader,
				IntentWriter: writer,
			}
			intent := rationaleBenchmarkMaterializationIntent()

			result, err := handler.Handle(context.Background(), intent)
			if err != nil {
				b.Fatalf("warm-up Handle() error = %v", err)
			}
			if result.CanonicalWrites != test.wantCanonicalWrites || writer.lastCount != test.wantCanonicalWrites {
				b.Fatalf("warm-up writes = result:%d writer:%d, want %d", result.CanonicalWrites, writer.lastCount, test.wantCanonicalWrites)
			}
			if loader.listFactsCalls != 0 || loader.listFactsByKindCalls != 1 {
				b.Fatalf("warm-up fact loads = ListFacts:%d ListFactsByKind:%d, want 0/1", loader.listFactsCalls, loader.listFactsByKindCalls)
			}

			b.ReportAllocs()
			b.ReportMetric(rationaleHandlerBenchmarkEntityCount, "content_entities/op")
			b.ReportMetric(float64(test.wantCanonicalWrites), "intents/op")
			b.ResetTimer()
			for range b.N {
				if _, err := handler.Handle(context.Background(), intent); err != nil {
					b.Fatalf("Handle() error = %v", err)
				}
			}
		})
	}
}

func rationaleHandlerBenchmarkFacts(positiveEvery int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, rationaleHandlerBenchmarkEntityCount+1)
	envelopes = append(envelopes, facts.Envelope{
		FactKind: factKindRepository,
		ScopeID:  "scope-rationale-benchmark",
		Payload: map[string]any{
			"repo_id": "repo-123", "local_path": "/repo",
			"source_run_id": "run-rationale-benchmark",
		},
	})
	for index := range rationaleHandlerBenchmarkEntityCount {
		payload := map[string]any{
			"repo_id":       "repo-123",
			"entity_id":     fmt.Sprintf("content-entity:%d", index),
			"entity_type":   "Function",
			"entity_name":   fmt.Sprintf("benchmark.Entity%d", index),
			"relative_path": fmt.Sprintf("src/entity_%05d.go", index),
		}
		if positiveEvery > 0 && index%positiveEvery == 0 {
			payload["entity_metadata"] = map[string]any{
				"rationale_comments": []any{map[string]any{
					"kind": "why",
					"text": fmt.Sprintf("benchmark rationale %d", index),
				}},
			}
		}
		envelopes = append(envelopes, facts.Envelope{FactKind: factKindContentEntity, Payload: payload})
	}
	return envelopes
}

func rationaleBenchmarkMaterializationIntent() Intent {
	now := time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC)
	return Intent{
		IntentID: "intent-rationale-benchmark", ScopeID: "scope-rationale-benchmark",
		GenerationID: "gen-rationale-benchmark", SourceSystem: "git",
		Domain: DomainRationaleMaterialization, EnqueuedAt: now, AvailableAt: now,
		Status: IntentStatusPending,
	}
}

type rationaleBenchmarkIntentWriter struct {
	lastCount int
}

func (w *rationaleBenchmarkIntentWriter) UpsertIntents(_ context.Context, rows []SharedProjectionIntentRow) error {
	w.lastCount = len(rows)
	return nil
}

type rationaleBenchmarkFactLoader struct {
	envelopes            []facts.Envelope
	listFactsCalls       int
	listFactsByKindCalls int
}

func (l *rationaleBenchmarkFactLoader) ListFacts(_ context.Context, _, _ string) ([]facts.Envelope, error) {
	l.listFactsCalls++
	return nil, fmt.Errorf("unbounded ListFacts must not serve rationale materialization")
}

func (l *rationaleBenchmarkFactLoader) ListFactsByKind(
	_ context.Context,
	_, _ string,
	factKinds []string,
) ([]facts.Envelope, error) {
	l.listFactsByKindCalls++
	if len(factKinds) != 2 || factKinds[0] != factKindRepository || factKinds[1] != factKindContentEntity {
		return nil, fmt.Errorf("fact kinds = %q, want [%s %s]", factKinds, factKindRepository, factKindContentEntity)
	}
	return l.envelopes, nil
}
