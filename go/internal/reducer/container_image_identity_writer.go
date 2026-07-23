// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

const containerImageIdentityFactKind = "reducer_container_image_identity"

// PostgresContainerImageIdentityWriter persists digest-keyed image identity
// decisions into the shared fact store.
type PostgresContainerImageIdentityWriter struct {
	DB  workloadIdentityExecer
	Now func() time.Time
}

// WriteContainerImageIdentityDecisions stores only canonical image identity
// decisions. Weak, missing, ambiguous, or stale tag outcomes stay diagnostic
// reducer output until a stronger source can prove digest identity.
func (w PostgresContainerImageIdentityWriter) WriteContainerImageIdentityDecisions(
	ctx context.Context,
	write ContainerImageIdentityWrite,
) (ContainerImageIdentityWriteResult, error) {
	if w.DB == nil {
		return ContainerImageIdentityWriteResult{}, fmt.Errorf("container image identity database is required")
	}

	now := reducerWriterNow(w.Now)
	decisions := containerImageIdentityCanonicalDecisions(write.Decisions)
	collectorKind := reducerFactCollectorKind(write.SourceSystem)
	rows := make([]reducerFactRow, 0, len(decisions))
	for _, decision := range decisions {
		canonicalID := canonicalContainerImageIdentityID(write, decision)
		payloadJSON, err := json.Marshal(containerImageIdentityPayload(write, decision, canonicalID))
		if err != nil {
			return ContainerImageIdentityWriteResult{}, fmt.Errorf("marshal container image identity payload: %w", err)
		}
		rows = append(rows, reducerFactRow{
			FactID:           containerImageIdentityFactID(write, decision),
			ScopeID:          write.ScopeID,
			GenerationID:     write.GenerationID,
			FactKind:         containerImageIdentityFactKind,
			StableFactKey:    containerImageIdentityStableFactKey(write, decision),
			CollectorKind:    collectorKind,
			SourceConfidence: facts.SourceConfidenceInferred,
			SourceSystem:     write.SourceSystem,
			SourceFactKey:    write.IntentID,
			ObservedAt:       now,
			IngestedAt:       now,
			Payload:          string(payloadJSON),
		})
	}
	// Bounded chunked bulk insert: canonical decisions are upserted in
	// O(N/batchSize) round-trips rather than one ExecContext per decision.
	if err := reducerBatchInsertFacts(ctx, w.DB, rows); err != nil {
		return ContainerImageIdentityWriteResult{}, fmt.Errorf("write container image identity fact: %w", err)
	}

	return ContainerImageIdentityWriteResult{
		CanonicalWrites: len(decisions),
		EvidenceSummary: fmt.Sprintf("wrote container image identity decisions %d", len(decisions)),
	}, nil
}

func containerImageIdentityFactID(
	write ContainerImageIdentityWrite,
	decision ContainerImageIdentityDecision,
) string {
	return containerImageIdentityFactKind + ":" + facts.StableID(
		containerImageIdentityFactKind,
		containerImageIdentityIdentity(write, decision),
	)
}

func containerImageIdentityStableFactKey(
	write ContainerImageIdentityWrite,
	decision ContainerImageIdentityDecision,
) string {
	identity := containerImageIdentityIdentity(write, decision)
	return strings.Join([]string{
		"container_image_identity",
		strings.TrimSpace(fmt.Sprint(identity["scope_id"])),
		strings.TrimSpace(fmt.Sprint(identity["generation_id"])),
		strings.TrimSpace(fmt.Sprint(identity["image_ref"])),
		strings.TrimSpace(fmt.Sprint(identity["outcome"])),
	}, ":")
}

func canonicalContainerImageIdentityID(
	write ContainerImageIdentityWrite,
	decision ContainerImageIdentityDecision,
) string {
	return "canonical:" + containerImageIdentityStableFactKey(write, decision)
}

func containerImageIdentityIdentity(
	write ContainerImageIdentityWrite,
	decision ContainerImageIdentityDecision,
) map[string]any {
	return map[string]any{
		"scope_id":      strings.TrimSpace(write.ScopeID),
		"generation_id": strings.TrimSpace(write.GenerationID),
		"image_ref":     strings.TrimSpace(decision.ImageRef),
		"outcome":       string(decision.Outcome),
	}
}

func containerImageIdentityPayload(
	write ContainerImageIdentityWrite,
	decision ContainerImageIdentityDecision,
	canonicalID string,
) map[string]any {
	return map[string]any{
		"reducer_domain":             string(DomainContainerImageIdentity),
		"intent_id":                  write.IntentID,
		"scope_id":                   write.ScopeID,
		"generation_id":              write.GenerationID,
		"source_system":              write.SourceSystem,
		"cause":                      write.Cause,
		"image_ref":                  decision.ImageRef,
		"digest":                     decision.Digest,
		"repository_id":              decision.RepositoryID,
		"source_revision":            strings.TrimSpace(decision.SourceRevision),
		"source_revision_provenance": strings.TrimSpace(decision.SourceRevisionProvenance),
		"source_repository_ids": uniqueSortedStrings(
			decision.SourceRepositoryIDs,
		),
		"workload_ids":      uniqueSortedStrings(decision.WorkloadIDs),
		"service_ids":       uniqueSortedStrings(decision.ServiceIDs),
		"outcome":           string(decision.Outcome),
		"reason":            decision.Reason,
		"canonical_id":      canonicalID,
		"canonical_writes":  decision.CanonicalWrites,
		"evidence_fact_ids": uniqueSortedStrings(decision.EvidenceFactIDs),
		"identity_strength": decision.IdentityStrength,
		"publication_kind":  containerImageIdentityFactKind,
		"source_layers": []string{
			string(truth.LayerSourceDeclaration),
			string(truth.LayerObservedResource),
		},
	}
}
