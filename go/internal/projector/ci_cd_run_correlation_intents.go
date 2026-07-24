// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// cicdRunCorrelationCandidateFactKinds are the fact kinds
// CICDRunCorrelationHandler.Handle loads for one intent
// (go/internal/reducer/ci_cd_run_correlation.go's cicdRunCorrelationFactKinds,
// unexported outside package reducer, so this list is kept in sync by hand).
// Triggering on every kind the handler reads — not only ci.run — matters
// because loadFactsForKinds scopes strictly to one scope generation
// (generation_id equality, see facts_filtered.go): a ci.artifact that lands
// in a later generation than its ci.run must independently trigger its own
// intent, or the correlation would never see that generation's artifact
// evidence.
var cicdRunCorrelationCandidateFactKinds = []string{
	facts.CICDRunFactKind,
	facts.CICDArtifactFactKind,
	facts.CICDWorkflowImageEvidenceFactKind,
	facts.CICDEnvironmentObservationFactKind,
	facts.CICDTriggerEdgeFactKind,
	facts.CICDStepFactKind,
}

// buildCICDRunCorrelationReducerIntent enqueues one ci_cd_run_correlation
// reducer intent per scope generation that observed CI/CD run evidence.
//
// #5710: CICDRunCorrelationHandler has been registered and wired in
// cmd/reducer/main.go since the domain was added, but no builder here ever
// emitted Domain=ci_cd_run_correlation, so the handler was unreachable in
// production and list_ci_cd_run_correlations always returned zero outside
// unit tests and Ifá replay. This builder closes that gap.
//
// The correlation reads reducer_container_image_identity rows across scopes
// (the CI scope's run/artifact evidence joins against the OCI/cloud scope's
// identity decision — see cross_scope_dependencies.go's
// crossScopeDependencyCatalog). That cross-scope read races the identity
// generation's activation exactly the way #5423 documented for
// container_image_identity's own OCI-manifest join, so — mirroring the
// proven fix rather than the still-unwired #5709 readiness-defer substrate —
// go/cmd/bootstrap-index/bootstrap_pipeline.go's maintenance-pass reopen
// replays ci_cd_run_correlation after container_image_identity is
// materialized, once the identity generation is active.
func buildCICDRunCorrelationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	envelope, ok := index.firstAcrossKinds(cicdRunCorrelationTriggerFact, cicdRunCorrelationCandidateFactKinds...)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainCICDRunCorrelation,
		EntityKey:    "ci_cd_run_correlation:" + scopeValue.ScopeID,
		Reason:       "ci/cd run correlation evidence observed",
		FactID:       envelope.FactID,
		SourceSystem: cicdRunCorrelationSourceSystem(envelope),
	}, true
}

// cicdRunCorrelationTriggerFact accepts every envelope of the candidate
// kinds unconditionally: unlike container_image_identity's AWSRelationship
// and content_entity branches, none of the CI/CD fact kinds need a
// payload-derived filter to qualify as correlation evidence — the handler's
// own decode/quarantine pass (buildCICDRunCorrelationDecisionsWithQuarantine)
// is what rejects malformed facts, and a malformed fact should still trigger
// the intent so its quarantine is recorded rather than silently dropped
// before the handler ever sees it.
func cicdRunCorrelationTriggerFact(facts.Envelope) bool {
	return true
}

func cicdRunCorrelationSourceSystem(envelope facts.Envelope) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	return strings.TrimSpace(envelope.CollectorKind)
}
