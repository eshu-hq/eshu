// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildCICDRunCorrelationReducerIntent enqueues one ci_cd_run_correlation
// reducer intent per scope generation that observed a CI/CD run — i.e. that
// carries a ci.run fact, the correlation's join anchor.
//
// #5710: CICDRunCorrelationHandler has been registered and wired in
// cmd/reducer/main.go since the domain was added, but no builder here ever
// emitted Domain=ci_cd_run_correlation, so the handler was unreachable in
// production and list_ci_cd_run_correlations always returned zero outside
// unit tests and Ifá replay. This builder closes that gap.
//
// The trigger is deliberately narrower than the full set of fact kinds
// CICDRunCorrelationHandler.Handle loads for one intent
// (cicdRunCorrelationFactKinds in ci_cd_run_correlation.go: ci.run,
// ci.artifact, ci.workflow_image_evidence, ci.environment_observation,
// ci.trigger_edge, ci.step) — it fires only on ci.run, not on any of the
// other five kinds alone. buildCICDRunCorrelationDecisionsWithQuarantine
// only emits a decision for evidence anchored by a ci.run
// (ci_cd_run_correlation_decode.go: `if ev.run.FactID == "" { continue }`),
// and Handle's fact load is scoped strictly to the triggering intent's own
// (scope, generation) — loadFactsForKinds is a generation_id equality
// filter (facts_filtered.go), not cumulative across generations. So an
// artifact-only generation's intent (no ci.run) would load only the
// artifact, the decode loop would produce zero decisions, and the intent
// would still succeed: a silent no-op that discards the artifact's evidence
// rather than deferring it, because nothing ever re-visits that generation
// once the intent has succeeded. Anchoring on ci.run avoids enqueuing that
// wasted intent. When ci.run and ci.artifact land in the SAME generation
// (the common case), Handle's own bulk load of that generation still picks
// up the co-located artifact — no separate per-artifact trigger is needed.
// Correlating a later-generation artifact against an earlier-generation run
// is a real gap the single-generation handler cannot close; filed as a
// follow-up (https://github.com/eshu-hq/eshu/issues/5766 tracks a related
// join-key gap in the same handler; the cross-generation load gap itself
// needs its own follow-up before it can be implemented).
//
// The correlation also reads reducer_container_image_identity rows across
// scopes (the CI scope's run/artifact evidence joins against the OCI/cloud
// scope's identity decision — see cross_scope_dependencies.go's
// crossScopeDependencyCatalog). That cross-scope read races the identity
// generation's activation exactly the way #5423 documented for
// container_image_identity's own OCI-manifest join.
// go/cmd/bootstrap-index/bootstrap_pipeline.go's maintenance-pass reopen
// lists ci_cd_run_correlation alongside container_image_identity as a
// best-effort, idempotent re-attempt — it does NOT guarantee the identity
// row commits before the correlation's reopened intent runs (reopening
// marks rows pending; there is no drain barrier between domains in the same
// reopen call), so the outcome ci_cd_run_correlation lands on after a
// reopen is not deterministic. minimum_results:1 on list_ci_cd_run_correlations
// does not depend on this: the domain writes a durable decision fact for
// every outcome (exact/derived/ambiguous/unresolved/rejected), so a row
// exists from the very first, non-reopened execution regardless.
func buildCICDRunCorrelationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	envelope, ok := index.firstOfKind(facts.CICDRunFactKind)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainCICDRunCorrelation,
		EntityKey:    "ci_cd_run_correlation:" + scopeValue.ScopeID,
		Reason:       "ci/cd run evidence observed",
		FactID:       envelope.FactID,
		SourceSystem: cicdRunCorrelationSourceSystem(envelope),
	}, true
}

func cicdRunCorrelationSourceSystem(envelope facts.Envelope) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	return strings.TrimSpace(envelope.CollectorKind)
}
