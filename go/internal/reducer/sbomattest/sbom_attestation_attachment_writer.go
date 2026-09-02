// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomattest

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

// SBOMAttestationAttachmentFactKind aliases the fact kind this family writes
// its attachment decisions under. See
// [reducercontract.SBOMAttestationAttachmentFactKind]: it is declared in
// contract, not here, because the reducer root's supply_chain_impact family
// (staying in root) joins against it directly too, in its EvidencePath
// construction and its active-fact-kind switches, and a family package must
// not import the reducer root to reach a root-declared constant.
const SBOMAttestationAttachmentFactKind = reducercontract.SBOMAttestationAttachmentFactKind

// PostgresSBOMAttestationAttachmentWriter stores reducer-owned SBOM and
// attestation attachment decisions in the shared fact store.
type PostgresSBOMAttestationAttachmentWriter struct {
	DB  factwrite.Execer
	Now func() time.Time
}

// WriteSBOMAttestationAttachments persists every attachment status so callers
// can distinguish verified, unverified, parse-only, mismatch, unknown, and
// unparseable evidence without collapsing trust into a boolean.
func (w PostgresSBOMAttestationAttachmentWriter) WriteSBOMAttestationAttachments(
	ctx context.Context,
	write SBOMAttestationAttachmentWrite,
) (SBOMAttestationAttachmentWriteResult, error) {
	if w.DB == nil {
		return SBOMAttestationAttachmentWriteResult{}, fmt.Errorf("sbom attestation attachment database is required")
	}
	now := factwrite.Now(w.Now)
	collectorKind := factwrite.CollectorKind(write.SourceSystem)
	rows := make([]factwrite.Row, 0, len(write.Decisions))
	for _, decision := range write.Decisions {
		payloadJSON, err := json.Marshal(sbomAttestationAttachmentPayload(write, decision))
		if err != nil {
			return SBOMAttestationAttachmentWriteResult{}, fmt.Errorf("marshal sbom attestation attachment payload: %w", err)
		}
		rows = append(rows, factwrite.Row{
			FactID:           sbomAttestationAttachmentFactID(write, decision),
			ScopeID:          write.ScopeID,
			GenerationID:     write.GenerationID,
			FactKind:         SBOMAttestationAttachmentFactKind,
			StableFactKey:    sbomAttestationAttachmentStableFactKey(write, decision),
			CollectorKind:    collectorKind,
			SourceConfidence: facts.SourceConfidenceInferred,
			SourceSystem:     write.SourceSystem,
			SourceFactKey:    write.IntentID,
			ObservedAt:       now,
			IngestedAt:       now,
			Payload:          string(payloadJSON),
		})
	}
	// Bounded chunked bulk insert: every attachment status is upserted in
	// O(N/batchSize) round-trips instead of one ExecContext per decision.
	if err := factwrite.BatchInsertFacts(ctx, w.DB, rows); err != nil {
		return SBOMAttestationAttachmentWriteResult{}, fmt.Errorf("write sbom attestation attachment fact: %w", err)
	}
	canonicalWrites := sbomAttestationAttachmentCanonicalWrites(write.Decisions)
	return SBOMAttestationAttachmentWriteResult{
		CanonicalWrites: canonicalWrites,
		FactsWritten:    len(write.Decisions),
		EvidenceSummary: fmt.Sprintf("wrote sbom attestation attachments=%d canonical_writes=%d", len(write.Decisions), canonicalWrites),
	}, nil
}

func sbomAttestationAttachmentFactID(
	write SBOMAttestationAttachmentWrite,
	decision SBOMAttestationAttachmentDecision,
) string {
	return SBOMAttestationAttachmentFactKind + ":" + facts.StableID(
		SBOMAttestationAttachmentFactKind,
		sbomAttestationAttachmentIdentity(write, decision),
	)
}

func sbomAttestationAttachmentStableFactKey(
	write SBOMAttestationAttachmentWrite,
	decision SBOMAttestationAttachmentDecision,
) string {
	identity := sbomAttestationAttachmentIdentity(write, decision)
	return strings.Join([]string{
		"sbom_attestation_attachment",
		strings.TrimSpace(fmt.Sprint(identity["scope_id"])),
		strings.TrimSpace(fmt.Sprint(identity["generation_id"])),
		strings.TrimSpace(fmt.Sprint(identity["document_id"])),
	}, ":")
}

func sbomAttestationAttachmentIdentity(
	write SBOMAttestationAttachmentWrite,
	decision SBOMAttestationAttachmentDecision,
) map[string]any {
	return map[string]any{
		"scope_id":      strings.TrimSpace(write.ScopeID),
		"generation_id": strings.TrimSpace(write.GenerationID),
		"document_id":   strings.TrimSpace(decision.DocumentID),
	}
}

func sbomAttestationAttachmentPayload(
	write SBOMAttestationAttachmentWrite,
	decision SBOMAttestationAttachmentDecision,
) map[string]any {
	return map[string]any{
		"reducer_domain":                            string(reducercontract.DomainSBOMAttestationAttachment),
		"intent_id":                                 write.IntentID,
		"scope_id":                                  write.ScopeID,
		"generation_id":                             write.GenerationID,
		"source_system":                             write.SourceSystem,
		"cause":                                     write.Cause,
		"document_id":                               decision.DocumentID,
		"document_digest":                           decision.DocumentDigest,
		"subject_digest":                            decision.SubjectDigest,
		"attachment_status":                         string(decision.AttachmentStatus),
		"parse_status":                              decision.ParseStatus,
		"verification_status":                       decision.VerificationStatus,
		"verification_policy":                       decision.VerificationPolicy,
		"artifact_kind":                             decision.ArtifactKind,
		"format":                                    decision.Format,
		"spec_version":                              decision.SpecVersion,
		"reason":                                    decision.Reason,
		"attachment_scope":                          decision.AttachmentScope,
		"canonical_writes":                          decision.CanonicalWrites,
		"component_count":                           decision.ComponentCount,
		"component_evidence":                        decision.ComponentEvidence,
		"dependency_relationship_count":             decision.DependencyRelationshipCount,
		"dependency_relationship_evidence":          decision.DependencyRelationshipEvidence,
		"external_reference_count":                  decision.ExternalReferenceCount,
		"external_reference_evidence":               decision.ExternalReferenceEvidence,
		"slsa_provenance_predicate_type":            decision.SLSAProvenancePredicateType,
		"slsa_provenance_builder_id":                decision.SLSAProvenanceBuilderID,
		"slsa_provenance_materials":                 decision.SLSAProvenanceMaterials,
		"slsa_provenance_material_count":            decision.SLSAProvenanceMaterialCount,
		"slsa_provenance_materials_truncated":       decision.SLSAProvenanceMaterialsTruncated,
		"slsa_provenance_config_source_uri":         decision.SLSAProvenanceConfigSourceURI,
		"slsa_provenance_config_source_entry_point": decision.SLSAProvenanceConfigSourceEntryPoint,
		"slsa_provenance_config_source_digest":      decision.SLSAProvenanceConfigSourceDigest,
		"repository_ids":                            payloadcore.UniqueSortedStrings(decision.RepositoryIDs),
		"workload_ids":                              payloadcore.UniqueSortedStrings(decision.WorkloadIDs),
		"service_ids":                               payloadcore.UniqueSortedStrings(decision.ServiceIDs),
		"warning_summaries":                         payloadcore.UniqueSortedStrings(decision.WarningSummaries),
		"warning_summary_count":                     decision.WarningSummaryCount,
		"evidence_fact_ids":                         payloadcore.UniqueSortedStrings(decision.EvidenceFactIDs),
		"missing_evidence":                          sbomAttestationAttachmentStrings(decision.MissingEvidence),
		"source_layer_kinds":                        payloadcore.UniqueSortedStrings(decision.SourceLayerKinds),
		"source_layers":                             sbomAttestationAttachmentSourceLayers(decision),
	}
}

func sbomAttestationAttachmentStrings(values []string) []string {
	out := payloadcore.UniqueSortedStrings(values)
	if out == nil {
		return []string{}
	}
	return out
}

func sbomAttestationAttachmentSourceLayers(decision SBOMAttestationAttachmentDecision) []string {
	layers := []string{string(truth.LayerSourceDeclaration)}
	for _, kind := range decision.SourceLayerKinds {
		if kind == "observed_resource" {
			layers = append(layers, string(truth.LayerObservedResource))
		}
	}
	if decision.CanonicalWrites > 0 {
		layers = append(layers, string(truth.LayerObservedResource))
	}
	return payloadcore.UniqueSortedStrings(layers)
}
