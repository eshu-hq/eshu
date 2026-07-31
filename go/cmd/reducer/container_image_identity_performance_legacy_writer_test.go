// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build perf5854_main

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const containerImageIdentityPerfFactKind = "reducer_container_image_identity"

type containerImageIdentityPerfLegacyWriter struct {
	store *postgres.FactStore
}

func (w containerImageIdentityPerfLegacyWriter) WriteContainerImageIdentityDecisions(
	ctx context.Context,
	write reducer.ContainerImageIdentityWrite,
) (reducer.ContainerImageIdentityWriteResult, error) {
	envelopes := make([]facts.Envelope, 0, len(write.Decisions))
	for _, decision := range write.Decisions {
		if decision.Outcome != reducer.ContainerImageIdentityExactDigest &&
			decision.Outcome != reducer.ContainerImageIdentityTagResolved {
			continue
		}
		identity := map[string]any{
			"scope_id":      strings.TrimSpace(write.ScopeID),
			"generation_id": strings.TrimSpace(write.GenerationID),
			"image_ref":     strings.TrimSpace(decision.ImageRef),
			"outcome":       string(decision.Outcome),
		}
		stableKey := strings.Join([]string{
			"container_image_identity",
			write.ScopeID,
			write.GenerationID,
			decision.ImageRef,
			string(decision.Outcome),
		}, ":")
		canonicalID := "canonical:" + stableKey
		envelopes = append(envelopes, facts.Envelope{
			FactID: containerImageIdentityPerfFactKind + ":" +
				facts.StableID(containerImageIdentityPerfFactKind, identity),
			ScopeID:          write.ScopeID,
			GenerationID:     write.GenerationID,
			FactKind:         containerImageIdentityPerfFactKind,
			StableFactKey:    stableKey,
			SchemaVersion:    "1.0.0",
			CollectorKind:    "reducer",
			FencingToken:     write.EvidenceAsOf.UTC().UnixMicro(),
			SourceConfidence: facts.SourceConfidenceInferred,
			ObservedAt:       write.EvidenceAsOf,
			Payload: map[string]any{
				"reducer_domain":                  string(reducer.DomainContainerImageIdentity),
				"intent_id":                       write.IntentID,
				"scope_id":                        write.ScopeID,
				"generation_id":                   write.GenerationID,
				"source_system":                   write.SourceSystem,
				"cause":                           write.Cause,
				"image_ref":                       decision.ImageRef,
				"digest":                          decision.Digest,
				"repository_id":                   decision.RepositoryID,
				"source_revision":                 strings.TrimSpace(decision.SourceRevision),
				"source_revision_provenance":      strings.TrimSpace(decision.SourceRevisionProvenance),
				"source_repository_ids":           containerImageIdentityPerfUniqueStrings(decision.SourceRepositoryIDs),
				"build_provenance_repository_ids": containerImageIdentityPerfUniqueStrings(decision.BuildProvenanceRepositoryIDs),
				"workload_ids":                    containerImageIdentityPerfUniqueStrings(decision.WorkloadIDs),
				"service_ids":                     containerImageIdentityPerfUniqueStrings(decision.ServiceIDs),
				"outcome":                         string(decision.Outcome),
				"reason":                          decision.Reason,
				"canonical_id":                    canonicalID,
				"canonical_writes":                decision.CanonicalWrites,
				"evidence_fact_ids":               containerImageIdentityPerfUniqueStrings(decision.EvidenceFactIDs),
				"identity_strength":               decision.IdentityStrength,
				"publication_kind":                containerImageIdentityPerfFactKind,
				"source_layers":                   []string{"source_declaration", "observed_resource"},
			},
			SourceRef: facts.Ref{
				SourceSystem: write.SourceSystem,
				ScopeID:      write.ScopeID,
				GenerationID: write.GenerationID,
				FactKey:      write.IntentID,
			},
		})
	}
	if err := w.store.UpsertFacts(ctx, envelopes); err != nil {
		return reducer.ContainerImageIdentityWriteResult{}, fmt.Errorf(
			"write legacy performance container image identities: %w",
			err,
		)
	}
	return reducer.ContainerImageIdentityWriteResult{
		CanonicalWrites: len(envelopes),
		EvidenceSummary: fmt.Sprintf(
			"wrote container image identity decisions %d",
			len(envelopes),
		),
	}, nil
}

func containerImageIdentityPerfUniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
