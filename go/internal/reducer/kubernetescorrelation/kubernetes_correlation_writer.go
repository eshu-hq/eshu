// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetescorrelation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

const kubernetesCorrelationFactKind = "reducer_kubernetes_correlation"

// PostgresKubernetesCorrelationWriter stores reducer-owned Kubernetes
// correlation decisions in the shared fact store. It writes through the
// canonical reducer fact insert path with a stable, retry-idempotent identity —
// no new table and no schema DDL.
type PostgresKubernetesCorrelationWriter struct {
	DB  factwrite.Execer
	Now func() time.Time
}

// WriteKubernetesCorrelations persists every correlation outcome so callers can
// distinguish exact ownership, derived image links, ambiguous selectors,
// missing sources, stale sources, and rejected weak refs, each with a drift
// kind. Repeated writes of the same decision converge on one fact_id, making the
// write safe under reducer retries and reprojection.
func (w PostgresKubernetesCorrelationWriter) WriteKubernetesCorrelations(
	ctx context.Context,
	write KubernetesCorrelationWrite,
) (KubernetesCorrelationWriteResult, error) {
	if w.DB == nil {
		return KubernetesCorrelationWriteResult{}, fmt.Errorf("kubernetes correlation database is required")
	}
	now := factwrite.Now(w.Now)
	for _, decision := range write.Decisions {
		payload := kubernetesCorrelationPayload(write, decision)
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return KubernetesCorrelationWriteResult{}, fmt.Errorf("marshal kubernetes correlation payload: %w", err)
		}
		if _, err := w.DB.ExecContext(
			ctx,
			factwrite.SingleInsertQuery,
			kubernetesCorrelationFactID(write, decision),
			write.ScopeID,
			write.GenerationID,
			kubernetesCorrelationFactKind,
			kubernetesCorrelationStableFactKey(write, decision),
			factwrite.CollectorKind(write.SourceSystem),
			facts.SourceConfidenceInferred,
			write.SourceSystem,
			write.IntentID,
			nil,
			nil,
			now,
			now,
			false,
			payloadJSON,
		); err != nil {
			return KubernetesCorrelationWriteResult{}, fmt.Errorf("write kubernetes correlation fact: %w", err)
		}
	}
	return KubernetesCorrelationWriteResult{
		FactsWritten:    len(write.Decisions),
		EvidenceSummary: fmt.Sprintf("wrote kubernetes correlations=%d", len(write.Decisions)),
	}, nil
}

func kubernetesCorrelationFactID(
	write KubernetesCorrelationWrite,
	decision KubernetesCorrelationDecision,
) string {
	return kubernetesCorrelationFactKind + ":" + facts.StableID(
		kubernetesCorrelationFactKind,
		kubernetesCorrelationIdentity(write, decision),
	)
}

func kubernetesCorrelationStableFactKey(
	write KubernetesCorrelationWrite,
	decision KubernetesCorrelationDecision,
) string {
	identity := kubernetesCorrelationIdentity(write, decision)
	return strings.Join([]string{
		"kubernetes_correlation",
		strings.TrimSpace(fmt.Sprint(identity["scope_id"])),
		strings.TrimSpace(fmt.Sprint(identity["generation_id"])),
		strings.TrimSpace(fmt.Sprint(identity["cluster_id"])),
		strings.TrimSpace(fmt.Sprint(identity["workload_object_id"])),
		strings.TrimSpace(fmt.Sprint(identity["subject"])),
	}, ":")
}

// kubernetesCorrelationIdentity is the stable identity tuple for a correlation
// decision. Image decisions key on the workload plus image reference; identity
// edge decisions key on the workload plus the directed edge key, so retries and
// reprojections converge on one fact per decision.
func kubernetesCorrelationIdentity(
	write KubernetesCorrelationWrite,
	decision KubernetesCorrelationDecision,
) map[string]any {
	subject := strings.TrimSpace(decision.ImageRef)
	if subject == "" {
		subject = strings.TrimSpace(decision.IdentityEdgeKey)
	}
	return map[string]any{
		"scope_id":           strings.TrimSpace(write.ScopeID),
		"generation_id":      strings.TrimSpace(write.GenerationID),
		"cluster_id":         strings.TrimSpace(decision.ClusterID),
		"workload_object_id": strings.TrimSpace(decision.WorkloadObjectID),
		"subject":            subject,
	}
}

func kubernetesCorrelationPayload(
	write KubernetesCorrelationWrite,
	decision KubernetesCorrelationDecision,
) map[string]any {
	return map[string]any{
		"reducer_domain":           string(reducercontract.DomainKubernetesCorrelation),
		"intent_id":                write.IntentID,
		"scope_id":                 write.ScopeID,
		"generation_id":            write.GenerationID,
		"source_system":            write.SourceSystem,
		"cause":                    write.Cause,
		"cluster_id":               decision.ClusterID,
		"workload_object_id":       decision.WorkloadObjectID,
		"namespace":                decision.Namespace,
		"workload_name":            decision.WorkloadName,
		"workload_uid":             decision.WorkloadUID,
		"image_ref":                decision.ImageRef,
		"source_digest":            decision.SourceDigest,
		"join_mode":                decision.JoinMode,
		"identity_edge_key":        decision.IdentityEdgeKey,
		"relationship_type":        decision.RelationshipType,
		"outcome":                  string(decision.Outcome),
		"drift_kind":               decision.DriftKind,
		"reason":                   decision.Reason,
		"non_promotion":            decision.NonPromotion,
		"provenance_only":          decision.ProvenanceOnly,
		"candidate_source_digests": payloadcore.UniqueSortedStrings(decision.CandidateSourceDigests),
		"warnings":                 payloadcore.UniqueSortedStrings(decision.Warnings),
		"evidence_fact_ids":        payloadcore.UniqueSortedStrings(decision.EvidenceFactIDs),
		"source_layers":            []string{string(truth.LayerObservedResource)},
	}
}
