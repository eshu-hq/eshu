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

const sbomAttestationAttachmentFactKind = "reducer_sbom_attestation_attachment"

// PostgresSBOMAttestationAttachmentWriter stores reducer-owned SBOM and
// attestation attachment decisions in the shared fact store.
type PostgresSBOMAttestationAttachmentWriter struct {
	DB  workloadIdentityExecer
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
	now := reducerWriterNow(w.Now)
	for _, decision := range write.Decisions {
		payloadJSON, err := json.Marshal(sbomAttestationAttachmentPayload(write, decision))
		if err != nil {
			return SBOMAttestationAttachmentWriteResult{}, fmt.Errorf("marshal sbom attestation attachment payload: %w", err)
		}
		if _, err := w.DB.ExecContext(
			ctx,
			canonicalReducerFactInsertQuery,
			sbomAttestationAttachmentFactID(write, decision),
			write.ScopeID,
			write.GenerationID,
			sbomAttestationAttachmentFactKind,
			sbomAttestationAttachmentStableFactKey(write, decision),
			reducerFactCollectorKind(write.SourceSystem),
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
			return SBOMAttestationAttachmentWriteResult{}, fmt.Errorf("write sbom attestation attachment fact: %w", err)
		}
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
	return sbomAttestationAttachmentFactKind + ":" + facts.StableID(
		sbomAttestationAttachmentFactKind,
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
		"reducer_domain":      string(DomainSBOMAttestationAttachment),
		"intent_id":           write.IntentID,
		"scope_id":            write.ScopeID,
		"generation_id":       write.GenerationID,
		"source_system":       write.SourceSystem,
		"cause":               write.Cause,
		"document_id":         decision.DocumentID,
		"document_digest":     decision.DocumentDigest,
		"subject_digest":      decision.SubjectDigest,
		"attachment_status":   string(decision.AttachmentStatus),
		"parse_status":        decision.ParseStatus,
		"verification_status": decision.VerificationStatus,
		"verification_policy": decision.VerificationPolicy,
		"artifact_kind":       decision.ArtifactKind,
		"format":              decision.Format,
		"spec_version":        decision.SpecVersion,
		"reason":              decision.Reason,
		"canonical_writes":    decision.CanonicalWrites,
		"component_count":     decision.ComponentCount,
		"component_evidence":  decision.ComponentEvidence,
		"warning_summaries":   uniqueSortedStrings(decision.WarningSummaries),
		"evidence_fact_ids":   uniqueSortedStrings(decision.EvidenceFactIDs),
		"source_layer_kinds":  uniqueSortedStrings(decision.SourceLayerKinds),
		"source_layers":       sbomAttestationAttachmentSourceLayers(decision),
	}
}

func sbomAttestationAttachmentSourceLayers(decision SBOMAttestationAttachmentDecision) []string {
	layers := []string{string(truth.LayerSourceDeclaration)}
	if decision.CanonicalWrites > 0 {
		layers = append(layers, string(truth.LayerObservedResource))
	}
	return uniqueSortedStrings(layers)
}
