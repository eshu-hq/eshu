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

const cicdRunCorrelationFactKind = "reducer_ci_cd_run_correlation"

// PostgresCICDRunCorrelationWriter stores reducer-owned CI/CD correlation
// decisions in the shared fact store.
type PostgresCICDRunCorrelationWriter struct {
	DB  workloadIdentityExecer
	Now func() time.Time
}

// WriteCICDRunCorrelations persists exact, derived, ambiguous, unresolved, and
// rejected run decisions so callers can see both truth and suppressed evidence.
func (w PostgresCICDRunCorrelationWriter) WriteCICDRunCorrelations(
	ctx context.Context,
	write CICDRunCorrelationWrite,
) (CICDRunCorrelationWriteResult, error) {
	if w.DB == nil {
		return CICDRunCorrelationWriteResult{}, fmt.Errorf("ci/cd run correlation database is required")
	}
	now := reducerWriterNow(w.Now)
	for _, decision := range write.Decisions {
		payload := cicdRunCorrelationPayload(write, decision)
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return CICDRunCorrelationWriteResult{}, fmt.Errorf("marshal ci/cd run correlation payload: %w", err)
		}
		if _, err := w.DB.ExecContext(
			ctx,
			canonicalReducerFactInsertQuery,
			cicdRunCorrelationFactID(write, decision),
			write.ScopeID,
			write.GenerationID,
			cicdRunCorrelationFactKind,
			cicdRunCorrelationStableFactKey(write, decision),
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
			return CICDRunCorrelationWriteResult{}, fmt.Errorf("write ci/cd run correlation fact: %w", err)
		}
	}
	canonicalWrites := cicdRunCorrelationCanonicalWrites(write.Decisions)
	return CICDRunCorrelationWriteResult{
		CanonicalWrites: canonicalWrites,
		FactsWritten:    len(write.Decisions),
		EvidenceSummary: fmt.Sprintf("wrote ci/cd run correlations=%d canonical_writes=%d", len(write.Decisions), canonicalWrites),
	}, nil
}

func cicdRunCorrelationFactID(write CICDRunCorrelationWrite, decision CICDRunCorrelationDecision) string {
	return cicdRunCorrelationFactKind + ":" + facts.StableID(
		cicdRunCorrelationFactKind,
		cicdRunCorrelationIdentity(write, decision),
	)
}

func cicdRunCorrelationStableFactKey(write CICDRunCorrelationWrite, decision CICDRunCorrelationDecision) string {
	identity := cicdRunCorrelationIdentity(write, decision)
	return strings.Join([]string{
		"ci_cd_run_correlation",
		strings.TrimSpace(fmt.Sprint(identity["scope_id"])),
		strings.TrimSpace(fmt.Sprint(identity["generation_id"])),
		strings.TrimSpace(fmt.Sprint(identity["provider"])),
		strings.TrimSpace(fmt.Sprint(identity["run_id"])),
		strings.TrimSpace(fmt.Sprint(identity["run_attempt"])),
	}, ":")
}

func cicdRunCorrelationIdentity(write CICDRunCorrelationWrite, decision CICDRunCorrelationDecision) map[string]any {
	return map[string]any{
		"scope_id":      strings.TrimSpace(write.ScopeID),
		"generation_id": strings.TrimSpace(write.GenerationID),
		"provider":      strings.TrimSpace(decision.Provider),
		"run_id":        strings.TrimSpace(decision.RunID),
		"run_attempt":   strings.TrimSpace(decision.RunAttempt),
		"outcome":       string(decision.Outcome),
	}
}

func cicdRunCorrelationPayload(write CICDRunCorrelationWrite, decision CICDRunCorrelationDecision) map[string]any {
	return map[string]any{
		"reducer_domain":     string(DomainCICDRunCorrelation),
		"intent_id":          write.IntentID,
		"scope_id":           write.ScopeID,
		"generation_id":      write.GenerationID,
		"source_system":      write.SourceSystem,
		"cause":              write.Cause,
		"provider":           decision.Provider,
		"run_id":             decision.RunID,
		"run_attempt":        decision.RunAttempt,
		"repository_id":      decision.RepositoryID,
		"commit_sha":         decision.CommitSHA,
		"environment":        decision.Environment,
		"artifact_digest":    decision.ArtifactDigest,
		"image_ref":          decision.ImageRef,
		"outcome":            string(decision.Outcome),
		"reason":             decision.Reason,
		"provenance_only":    decision.ProvenanceOnly,
		"canonical_writes":   decision.CanonicalWrites,
		"canonical_target":   decision.CanonicalTarget,
		"correlation_kind":   decision.CorrelationKind,
		"evidence_fact_ids":  uniqueSortedStrings(decision.EvidenceFactIDs),
		"source_layer_kinds": uniqueSortedStrings(decision.SourceLayerKinds),
		"source_layers": []string{
			string(truth.LayerSourceDeclaration),
			string(truth.LayerObservedResource),
		},
	}
}
