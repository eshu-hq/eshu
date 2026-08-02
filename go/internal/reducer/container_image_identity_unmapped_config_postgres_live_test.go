// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestPostgresContainerImageIdentityHandlerKeepsUnmappedConfigWarningRepository(
	t *testing.T,
) {
	db := openContainerImageIdentityLivePostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	seedContainerImageIdentityLiveParents(t, ctx, db)

	const (
		affectedRef  = "registry.example.com/team/api:prod"
		unrelatedRef = "registry.example.com/team/worker:prod"
	)
	scopeFacts := []facts.Envelope{
		gitImageRefFact("git-unmapped-config-affected", affectedRef),
		gitImageRefFact("git-unmapped-config-unrelated", unrelatedRef),
	}
	decisions := BuildContainerImageIdentityDecisions(scopeFacts)
	write := ContainerImageIdentityWrite{
		IntentID:     containerImageIdentityLiveWorkItemID(containerImageIdentityLiveGeneration),
		ClaimEpoch:   1,
		ScopeID:      containerImageIdentityLiveScope,
		GenerationID: containerImageIdentityLiveGeneration,
		SourceSystem: "git",
		Cause:        "synthetic unmapped config warning live proof",
		EvidenceAsOf: time.Date(2026, time.July, 30, 23, 0, 0, 0, time.UTC),
		Decisions:    decisions,
	}
	legacyIDs := make(map[string]string, len(decisions))
	for _, decision := range decisions {
		outcome, ok := containerImageIdentityLegacyOutcome(decision)
		if !ok {
			t.Fatalf("legacy outcome missing for %q", decision.ImageRef)
		}
		legacy := containerImageIdentityDecisionWithOutcome(decision, outcome)
		legacyIDs[decision.ImageRef] = legacyContainerImageIdentityFactID(write, legacy)
	}
	write.LegacyFactIDs = []string{legacyIDs[affectedRef], legacyIDs[unrelatedRef]}
	cleanupContainerImageIdentityAtomicLiveWrite(t, db, write)
	t.Cleanup(func() { cleanupContainerImageIdentityAtomicLiveWrite(t, db, write) })
	for _, legacyID := range write.LegacyFactIDs {
		if err := reducerBatchInsertFacts(
			ctx,
			db,
			[]reducerFactRow{containerImageIdentityLegacyLiveRow(legacyID, 1, false)},
		); err != nil {
			t.Fatalf("seed legacy row %q: %v", legacyID, err)
		}
	}

	handler := ContainerImageIdentityHandler{
		FactLoader: &stubContainerImageIdentityFactLoader{
			scopeFacts: scopeFacts,
			warnings: []facts.Envelope{
				retirementWarningEnvelope(
					"config_blob_unavailable",
					retirementTestConfigDigest,
				),
			},
		},
		Writer: PostgresContainerImageIdentityWriter{
			DB:       db,
			Beginner: &containerImageIdentityAtomicLiveBeginner{db: db},
		},
		Now:                func() time.Time { return write.EvidenceAsOf },
		FencingTokenIssuer: &stubContainerImageIdentityFencingTokenIssuer{tokens: []int64{100}},
	}
	result, err := handler.Handle(ctx, Intent{
		IntentID:     write.IntentID,
		ClaimEpoch:   write.ClaimEpoch,
		Domain:       DomainContainerImageIdentity,
		ScopeID:      write.ScopeID,
		GenerationID: write.GenerationID,
		SourceSystem: write.SourceSystem,
		Cause:        write.Cause,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got := result.SubSignals["retire_held_config_blob_unavailable"]; got != 1 {
		t.Fatalf("held config-blob sub-signal = %v, want 1", got)
	}

	assertContainerImageIdentityAtomicLiveCount(
		t, ctx, db,
		`SELECT count(*) FROM fact_records
		  WHERE fact_id = $1 AND NOT is_tombstone`,
		1, legacyIDs[affectedRef],
	)
	assertContainerImageIdentityAtomicLiveCount(
		t, ctx, db,
		`SELECT count(*) FROM fact_records WHERE fact_id = $1`,
		0, legacyIDs[unrelatedRef],
	)
	assertContainerImageIdentityAtomicLiveCount(
		t, ctx, db,
		`SELECT count(*) FROM fact_records
		  WHERE fact_id = $1 AND is_tombstone`,
		1, containerImageIdentityFactID(write, decisionsByRef(decisions)[unrelatedRef]),
	)
	assertContainerImageIdentityAtomicLiveCount(
		t, ctx, db,
		`SELECT count(*) FROM fact_records WHERE fact_id = $1`,
		0, containerImageIdentityFactID(write, decisionsByRef(decisions)[affectedRef]),
	)
}
