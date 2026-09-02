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
// reducer intent per scope generation that observed a CI/CD run or artifact.
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
// ci.trigger_edge, ci.step) — it fires on ci.run and ci.artifact only. A run
// anchors a normal authoritative snapshot. An artifact without a co-located
// run is a domain patch: the handler rebuilds the complete bounded latest-live
// run snapshot from retained source evidence, applies the current artifact
// control rows, and writes the result into the artifact generation. This keeps
// unaffected runs visible under the active-generation read fence even if queue
// supersession prevented the preceding reducer work item from publishing.
// The other loaded kinds cannot independently establish this patch contract:
// workflow-image evidence is repository-scoped, and environment, trigger, and
// step evidence do not provide the artifact arrival signal #5770 addresses.
//
// The correlation also reads reducer_container_image_identity rows across
// scopes (the CI scope's run/artifact evidence joins against the OCI/cloud
// scope's identity decision — see cross_scope_dependencies.go's
// crossscope.dependencyCatalog). That cross-scope read races the identity
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
		envelope, ok = index.firstOfKind(facts.CICDArtifactFactKind)
		if !ok {
			return ReducerIntent{}, false
		}
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainCICDRunCorrelation,
		EntityKey:    "ci_cd_run_correlation:" + scopeValue.ScopeID,
		Reason:       "ci/cd run-scoped evidence observed",
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
