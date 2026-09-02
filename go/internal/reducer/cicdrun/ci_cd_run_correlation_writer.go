// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

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

// CICDRunCorrelationFactKind names the durable fact kind this writer
// publishes under.
//
// Exported (rather than package-private) because it names the shared
// vocabulary supply_chain_impact and other reducer-root domains read this
// family's canonical writes under (issue #6061); the reducer root keeps a
// forwarding alias so those call sites' spelling is unchanged.
const CICDRunCorrelationFactKind = "reducer_ci_cd_run_correlation"

// PostgresCICDRunCorrelationWriter stores reducer-owned CI/CD correlation
// decisions in the shared fact store.
type PostgresCICDRunCorrelationWriter struct {
	DB  factwrite.Execer
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
	now := factwrite.Now(w.Now)
	collectorKind := factwrite.CollectorKind(write.SourceSystem)
	rows := make([]factwrite.Row, 0, len(write.Decisions))
	for _, decision := range write.Decisions {
		payloadJSON, err := json.Marshal(cicdRunCorrelationPayload(write, decision))
		if err != nil {
			return CICDRunCorrelationWriteResult{}, fmt.Errorf("marshal ci/cd run correlation payload: %w", err)
		}
		rows = append(rows, factwrite.Row{
			FactID:           cicdRunCorrelationFactID(write, decision),
			ScopeID:          write.ScopeID,
			GenerationID:     write.GenerationID,
			FactKind:         CICDRunCorrelationFactKind,
			StableFactKey:    cicdRunCorrelationStableFactKey(write, decision),
			CollectorKind:    collectorKind,
			SourceConfidence: facts.SourceConfidenceInferred,
			SourceSystem:     write.SourceSystem,
			SourceFactKey:    write.IntentID,
			ObservedAt:       now,
			IngestedAt:       now,
			Payload:          string(payloadJSON),
		})
	}
	// Bounded chunked bulk insert: all decisions for the scope are upserted in
	// O(N/batchSize) round-trips instead of one ExecContext per decision, so a
	// large generation cannot monopolise a reducer worker with serial inserts.
	if err := factwrite.BatchInsertFacts(ctx, w.DB, rows); err != nil {
		return CICDRunCorrelationWriteResult{}, fmt.Errorf("write ci/cd run correlation fact: %w", err)
	}
	canonicalWrites := cicdRunCorrelationCanonicalWrites(write.Decisions)
	return CICDRunCorrelationWriteResult{
		CanonicalWrites: canonicalWrites,
		FactsWritten:    len(write.Decisions),
		EvidenceSummary: fmt.Sprintf("wrote ci/cd run correlations=%d canonical_writes=%d", len(write.Decisions), canonicalWrites),
	}, nil
}

func cicdRunCorrelationFactID(write CICDRunCorrelationWrite, decision CICDRunCorrelationDecision) string {
	return CICDRunCorrelationFactKind + ":" + facts.StableID(
		CICDRunCorrelationFactKind,
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
	}
}

func cicdRunCorrelationPayload(write CICDRunCorrelationWrite, decision CICDRunCorrelationDecision) map[string]any {
	return map[string]any{
		"reducer_domain":       string(reducercontract.DomainCICDRunCorrelation),
		"intent_id":            write.IntentID,
		"scope_id":             write.ScopeID,
		"generation_id":        write.GenerationID,
		"source_system":        write.SourceSystem,
		"cause":                write.Cause,
		"provider":             decision.Provider,
		"run_id":               decision.RunID,
		"run_attempt":          decision.RunAttempt,
		"repository_id":        decision.RepositoryID,
		"commit_sha":           decision.CommitSHA,
		"environment":          decision.Environment,
		"environment_evidence": decision.EnvironmentEvidence,
		"artifact_digest":      decision.ArtifactDigest,
		"image_ref":            decision.ImageRef,
		"outcome":              string(decision.Outcome),
		"reason":               decision.Reason,
		"provenance_only":      decision.ProvenanceOnly,
		"canonical_writes":     decision.CanonicalWrites,
		"canonical_target":     decision.CanonicalTarget,
		"correlation_kind":     decision.CorrelationKind,
		"evidence_fact_ids":    payloadcore.UniqueSortedStrings(decision.EvidenceFactIDs),
		"source_layer_kinds":   payloadcore.UniqueSortedStrings(decision.SourceLayerKinds),
		"source_layers":        cicdRunCorrelationSourceLayers(decision),
	}
}

func cicdRunCorrelationSourceLayers(decision CICDRunCorrelationDecision) []string {
	layers := []string{string(truth.LayerSourceDeclaration)}
	if decision.CanonicalTarget != "" || decision.Outcome == CICDRunCorrelationExact {
		layers = append(layers, string(truth.LayerObservedResource))
	}
	return payloadcore.UniqueSortedStrings(layers)
}
