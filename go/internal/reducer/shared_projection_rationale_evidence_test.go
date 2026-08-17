// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"reflect"
	"testing"
	"time"
)

type rationaleEvidenceCapturingWriter struct {
	retractSources []string
	writeSources   []string
}

func (w *rationaleEvidenceCapturingWriter) RetractEdges(
	_ context.Context,
	domain string,
	_ []SharedProjectionIntentRow,
	evidenceSource string,
) error {
	if domain == DomainRationaleEdges {
		w.retractSources = append(w.retractSources, evidenceSource)
	}
	return nil
}

func (w *rationaleEvidenceCapturingWriter) WriteEdges(
	_ context.Context,
	domain string,
	_ []SharedProjectionIntentRow,
	evidenceSource string,
) (SharedProjectionWriteReport, error) {
	if domain == DomainRationaleEdges {
		w.writeSources = append(w.writeSources, evidenceSource)
	}
	return SharedProjectionWriteReport{}, nil
}

func TestSharedProjectionRunnerPassesRationaleEvidenceSourceToRetractAndWrite(t *testing.T) {
	t.Parallel()

	reader := &fakeSharedIntentReader{intents: []SharedProjectionIntentRow{{
		IntentID:         "rationale-intent-1",
		ProjectionDomain: DomainRationaleEdges,
		PartitionKey:     "rationale:edge:1",
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		RepositoryID:     "repo-a",
		SourceRunID:      "run-1",
		GenerationID:     "gen-1",
		Payload: map[string]any{
			"action":           "upsert",
			"repo_id":          "repo-a",
			"rationale_uid":    "rationale:uid:WHY:abc",
			"target_entity_id": "content-entity:target",
			"comment_kind":     "WHY",
			"excerpt_hash":     "abc",
		},
		CreatedAt: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
	}}}
	edges := &rationaleEvidenceCapturingWriter{}
	runner := SharedProjectionRunner{
		IntentReader:    reader,
		LeaseManager:    &fakeLeaseManager{granted: true},
		EdgeWriter:      edges,
		AcceptedGen:     acceptedGenerationFixed("gen-1", true),
		ReadinessLookup: readinessLookupFixed(true, true),
		Config: SharedProjectionRunnerConfig{
			PartitionCount: 1,
			LeaseOwner:     "test-runner",
		},
	}

	result := runner.runOneCycle(context.Background())
	if got, want := result.ProcessedIntents, 1; got != want {
		t.Fatalf("ProcessedIntents = %d, want %d", got, want)
	}
	wantSources := []string{rationaleEvidenceSource}
	if !reflect.DeepEqual(edges.retractSources, wantSources) {
		t.Fatalf("retract evidence sources = %#v, want %#v", edges.retractSources, wantSources)
	}
	if !reflect.DeepEqual(edges.writeSources, wantSources) {
		t.Fatalf("write evidence sources = %#v, want %#v", edges.writeSources, wantSources)
	}
}
