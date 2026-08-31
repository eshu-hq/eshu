// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

type deploymentSourceMetadata struct {
	repoID     string
	confidence float64
	provenance string
}

// deploymentSourceOutcome names the classification
// deploymentSourceRelationshipOutcome assigns a resolved relationship: one of
// applyResolvedDeploymentSources's three skip guards, or the relationship
// survived all three. Named as strings, not an iota enum, because their only
// consumers are a map key (deploymentSourceGuardStats) and a log/metric
// attribute value -- a string is what both need, and an iota would just be
// converted back to one at every call site.
const (
	deploymentSourceOutcomeWrongType     = "wrong_type"
	deploymentSourceOutcomeMissingRepoID = "missing_repo_id"
	deploymentSourceOutcomeNoEvidence    = "no_deployment_evidence"
	// deploymentSourceOutcomePassedGuards names a relationship that survived
	// all three of applyResolvedDeploymentSources's guards -- NOT that it
	// enriched a candidate. A relationship can pass all three guards and
	// still enrich zero candidates, if its normalized appRepoID matches no
	// candidate's RepoID; that case is indistinguishable from real
	// enrichment at this classification (see logDeploymentSourceGuardStats's
	// doc comment for the residual gap this leaves, and why closing it is
	// not attempted here). Named passed_guards, not applied, because a
	// review thread on #6157 found the earlier name read as a claim this
	// classification does not make.
	deploymentSourceOutcomePassedGuards = "passed_guards"
)

// deploymentSourceRelationshipOutcome classifies one resolved relationship
// against applyResolvedDeploymentSources's three guards, in the same order
// the enrichment loop below checks them: wrong relationship type, missing an
// endpoint repo id, then missing recognized deployment evidence. This is the
// single place those three conditions are expressed -- the enrichment loop
// and deploymentSourceGuardStats (used by real production callers to log
// which guard fired) both call this, so the two can never silently disagree
// about what counts as a skip (#6149 follow-up item 6: three silent
// `continue`s here had no observability at all, so a zero-edges
// materialization could only be diagnosed by re-deriving this classification
// by hand).
func deploymentSourceRelationshipOutcome(relationship relationships.ResolvedRelationship) string {
	switch {
	case relationship.RelationshipType != relationships.RelDeploysFrom:
		return deploymentSourceOutcomeWrongType
	case relationship.SourceRepoID == "" || relationship.TargetRepoID == "":
		return deploymentSourceOutcomeMissingRepoID
	case !hasDeploymentEvidence(relationship.Details):
		return deploymentSourceOutcomeNoEvidence
	default:
		return deploymentSourceOutcomePassedGuards
	}
}

// deploymentSourceGuardStats is deploymentSourceRelationshipOutcome's tally
// across a batch of resolved relationships -- what a real production caller
// (one with a context, unlike applyResolvedDeploymentSources and
// ExtractDeployableUnitCorrelationRows, which stay side-effect-free for
// Ifá's direct-call contract; see ExtractDeployableUnitCorrelationRows's own
// doc comment) logs to name which guard actually produced a zero-edges
// outcome.
type deploymentSourceGuardStatsResult struct {
	total         int
	wrongType     int
	missingRepoID int
	noEvidence    int
	// passedGuards counts relationships that survived all three guards, not
	// relationships that enriched a candidate -- see
	// deploymentSourceOutcomePassedGuards's doc comment.
	passedGuards int
}

// deploymentSourceGuardStats computes deploymentSourceGuardStatsResult over
// resolved. Pure and side-effect-free like applyResolvedDeploymentSources
// itself -- it is a second, independent O(len(resolved)) pass callers run
// purely for diagnostics, not a replacement for the enrichment loop's own
// classification.
func deploymentSourceGuardStats(resolved []relationships.ResolvedRelationship) deploymentSourceGuardStatsResult {
	var stats deploymentSourceGuardStatsResult
	for _, relationship := range resolved {
		stats.total++
		switch deploymentSourceRelationshipOutcome(relationship) {
		case deploymentSourceOutcomeWrongType:
			stats.wrongType++
		case deploymentSourceOutcomeMissingRepoID:
			stats.missingRepoID++
		case deploymentSourceOutcomeNoEvidence:
			stats.noEvidence++
		case deploymentSourceOutcomePassedGuards:
			stats.passedGuards++
		}
	}
	return stats
}

// logDeploymentSourceGuardStats emits one structured log line naming which
// of deploymentSourceRelationshipOutcome's guards a batch of resolved
// relationships tripped, so a materialization that ends up enriching zero
// candidates with a deployment repo can be diagnosed from this line instead
// of the guard being re-derived by hand (#6149 follow-up item 6: three
// silent `continue`s in applyResolvedDeploymentSources had no observability
// at all -- two agents spent an evening reconstructing which one fired).
//
// Called by real production entry points ONLY -- never by
// applyResolvedDeploymentSources or ExtractDeployableUnitCorrelationRows
// themselves, both of which stay side-effect-free for Ifá's direct-call
// contract (ExtractDeployableUnitCorrelationRows's own doc comment states
// "performs no I/O and reads no other process-global state" as the reason it
// is safe for Ifá's deployable_unit_edges materialized-edge vacuity guard to
// call directly against a cataloged Odù). Logging is therefore a SEPARATE
// pass over resolved at each call site, not threaded through either pure
// function's return value -- an intentional trade of a second cheap
// O(len(resolved)) pass against not touching either function's signature or
// its documented purity contract.
//
// Warns (not merely informs) when resolved carries at least one
// deployment-typed relationship (wrongType < total) and none of them passed
// all three guards -- the actual "zero edges" failure mode this item exists
// to make diagnosable -- and otherwise logs at debug level.
//
// wrongType < total is load-bearing, not a nicety: a repo with dependency
// evidence but no ArgoCD/Helm/Kustomize evidence at all is the HEALTHY,
// overwhelmingly common case (this file's own evidence note calls wrong_type
// "the overwhelmingly common and entirely expected outcome"), and it makes
// passedGuards == 0 on every single call for that repo. Warning on that
// unconditionally (the original condition, `stats.passedGuards == 0`) fired
// on every healthy scope with dependencies and no deployment evidence,
// flooding the log with the exact WARN it was meant to make findable and
// training an operator to ignore it (#6157 review). Scoping to "at least one
// deployment-typed row existed, and none survived" is the actual failure
// this item was written for.
//
// passedGuards counts relationships that survived all three guards, not
// relationships that enriched a candidate -- a relationship can pass every
// guard and still enrich zero candidates, if its normalized appRepoID
// matches no candidate's RepoID. That "guards passed, nothing enriched"
// shape is NOT distinguished by this line; it logs identically to genuine
// enrichment (passedGuards > 0, DEBUG). Closing that gap would need either a
// second pass through applyResolvedDeploymentSources at each call site
// (doubling an already-priced ~81-85%-of-baseline cost, and needing its own
// benchmark proof) or a return-value change to a function whose signature
// Ifá's deployable_unit_edges vacuity guard depends on staying exactly as it
// is (ExtractDeployableUnitCorrelationRows's own doc comment). Neither is
// attempted here; the gap is named rather than left implied (#6157 review).
func logDeploymentSourceGuardStats(
	ctx context.Context,
	domain, scopeID, generationID string,
	resolved []relationships.ResolvedRelationship,
) {
	if len(resolved) == 0 {
		return
	}
	stats := deploymentSourceGuardStats(resolved)
	attrs := []any{
		log.Domain(domain),
		log.ScopeID(scopeID),
		log.GenerationID(generationID),
		slog.Int("resolved_total", stats.total),
		slog.Int("skipped_wrong_type", stats.wrongType),
		slog.Int("skipped_missing_repo_id", stats.missingRepoID),
		slog.Int("skipped_no_deployment_evidence", stats.noEvidence),
		slog.Int("passed_guards", stats.passedGuards),
	}
	if stats.passedGuards == 0 && stats.wrongType < stats.total {
		slog.WarnContext(ctx, "deployment source resolution consumed every deployment-source relationship", attrs...)
		return
	}
	slog.DebugContext(ctx, "deployment source resolution guard stats", attrs...)
}

func applyResolvedDeploymentSources(
	candidates []WorkloadCandidate,
	resolved []relationships.ResolvedRelationship,
) []WorkloadCandidate {
	if len(candidates) == 0 || len(resolved) == 0 {
		return candidates
	}

	deploymentReposBySource := make(map[string][]deploymentSourceMetadata, len(resolved))
	for _, relationship := range resolved {
		if deploymentSourceRelationshipOutcome(relationship) != deploymentSourceOutcomePassedGuards {
			continue
		}
		provenance := argoDeploymentProvenance(relationship.Details)
		if provenance == "" {
			provenance = "argocd_application_source"
		}

		// Direction depends on evidence kind. ArgoCD Applications,
		// Kustomize, and Helm originate in the deployment/control repo and
		// point at the deployed app. ApplicationSet deploy-source evidence is
		// normalized in the reverse direction when it is discovered.
		appRepoID, deployRepoID := relationship.SourceRepoID, relationship.TargetRepoID
		if isDeployRepoOriginatedEvidence(relationship.Details) {
			appRepoID, deployRepoID = relationship.TargetRepoID, relationship.SourceRepoID
		}

		metadata := deploymentSourceMetadata{
			repoID:     deployRepoID,
			confidence: relationship.Confidence,
			provenance: provenance,
		}
		deploymentReposBySource[appRepoID] = appendDeploymentSourceMetadata(
			deploymentReposBySource[appRepoID],
			metadata,
		)
	}

	if len(deploymentReposBySource) == 0 {
		return candidates
	}

	enriched := make([]WorkloadCandidate, len(candidates))
	for i, candidate := range candidates {
		enriched[i] = candidate
		for _, metadata := range deploymentReposBySource[candidate.RepoID] {
			enriched[i].DeploymentRepoIDs = appendUniqueString(enriched[i].DeploymentRepoIDs, metadata.repoID)
			if enriched[i].DeploymentRepoID == "" || metadata.confidence > enriched[i].Confidence {
				enriched[i].DeploymentRepoID = metadata.repoID
			}
			if metadata.confidence > enriched[i].Confidence {
				enriched[i].Confidence = metadata.confidence
			}
			enriched[i].Provenance = appendUniqueString(enriched[i].Provenance, metadata.provenance)
		}
		if len(deploymentReposBySource[candidate.RepoID]) > 0 {
			enriched[i].Classification = InferWorkloadClassification(enriched[i])
		}
	}

	return enriched
}

func appendDeploymentSourceMetadata(existing []deploymentSourceMetadata, candidate deploymentSourceMetadata) []deploymentSourceMetadata {
	if candidate.repoID == "" {
		return existing
	}
	for i, item := range existing {
		if item.repoID != candidate.repoID {
			continue
		}
		if candidate.confidence > item.confidence {
			existing[i] = candidate
		}
		return existing
	}
	return append(existing, candidate)
}

func applyResolvedProvisioningSources(
	candidates []WorkloadCandidate,
	resolved []relationships.ResolvedRelationship,
) []WorkloadCandidate {
	if len(candidates) == 0 || len(resolved) == 0 {
		return candidates
	}

	provisioningReposByTarget := make(map[string][]string, len(resolved))
	provisioningEvidenceKindsByTarget := make(map[string]map[string][]string, len(resolved))
	for _, relationship := range resolved {
		if relationship.RelationshipType != relationships.RelProvisionsDependencyFor {
			continue
		}
		if relationship.SourceRepoID == "" || relationship.TargetRepoID == "" {
			continue
		}
		provisioningReposByTarget[relationship.TargetRepoID] = appendUniqueString(
			provisioningReposByTarget[relationship.TargetRepoID],
			relationship.SourceRepoID,
		)
		for _, evidenceKind := range toStringSlice(relationship.Details["evidence_kinds"]) {
			if provisioningEvidenceKindsByTarget[relationship.TargetRepoID] == nil {
				provisioningEvidenceKindsByTarget[relationship.TargetRepoID] = make(map[string][]string)
			}
			provisioningEvidenceKindsByTarget[relationship.TargetRepoID][relationship.SourceRepoID] = appendUniqueString(
				provisioningEvidenceKindsByTarget[relationship.TargetRepoID][relationship.SourceRepoID],
				evidenceKind,
			)
		}
	}
	if len(provisioningReposByTarget) == 0 {
		return candidates
	}

	enriched := make([]WorkloadCandidate, len(candidates))
	for i, candidate := range candidates {
		enriched[i] = candidate
		enriched[i].ProvisioningEvidenceKinds = cloneStringSliceMap(candidate.ProvisioningEvidenceKinds)
		for _, repoID := range provisioningReposByTarget[candidate.RepoID] {
			enriched[i].ProvisioningRepoIDs = appendUniqueString(enriched[i].ProvisioningRepoIDs, repoID)
			for _, evidenceKind := range provisioningEvidenceKindsByTarget[candidate.RepoID][repoID] {
				if enriched[i].ProvisioningEvidenceKinds == nil {
					enriched[i].ProvisioningEvidenceKinds = make(map[string][]string)
				}
				enriched[i].ProvisioningEvidenceKinds[repoID] = appendUniqueString(
					enriched[i].ProvisioningEvidenceKinds[repoID],
					evidenceKind,
				)
			}
		}
	}

	return enriched
}

// hasDeploymentEvidence returns true when the resolved relationship carries
// evidence kinds that indicate a deployment source linkage (ArgoCD, Kustomize,
// or Helm).
func hasDeploymentEvidence(details map[string]any) bool {
	rawKinds, ok := details["evidence_kinds"]
	if !ok {
		return false
	}

	for _, kind := range toStringSlice(rawKinds) {
		switch relationships.EvidenceKind(kind) {
		case relationships.EvidenceKindArgoCDAppSource,
			relationships.EvidenceKindArgoCDApplicationSetDeploySource,
			relationships.EvidenceKindKustomizeResource,
			relationships.EvidenceKindHelmValues,
			relationships.EvidenceKindHelmChart:
			return true
		}
	}

	return false
}

// isDeployRepoOriginatedEvidence reports whether a resolved edge starts at the
// deployment or control repository and points at the deployed application.
// ArgoCD Application, Kustomize, and Helm evidence use that direction.
// ApplicationSet deploy-source evidence is normalized in the reverse direction.
func isDeployRepoOriginatedEvidence(details map[string]any) bool {
	rawKinds, ok := details["evidence_kinds"]
	if !ok {
		return false
	}
	for _, kind := range toStringSlice(rawKinds) {
		switch relationships.EvidenceKind(kind) {
		case relationships.EvidenceKindArgoCDAppSource,
			relationships.EvidenceKindKustomizeResource,
			relationships.EvidenceKindHelmValues,
			relationships.EvidenceKindHelmChart:
			return true
		}
	}
	return false
}

func argoDeploymentProvenance(details map[string]any) string {
	for _, kind := range toStringSlice(details["evidence_kinds"]) {
		switch relationships.EvidenceKind(kind) {
		case relationships.EvidenceKindArgoCDApplicationSetDeploySource:
			return "argocd_applicationset_deploy_source"
		case relationships.EvidenceKindArgoCDAppSource:
			return "argocd_application_source"
		case relationships.EvidenceKindKustomizeResource:
			return "kustomize_resource"
		case relationships.EvidenceKindHelmValues, relationships.EvidenceKindHelmChart:
			return "helm_deployment"
		}
	}
	return ""
}

// toStringSlice forwards to [payloadcore.ToStringSlice].
func toStringSlice(value any) []string {
	return payloadcore.ToStringSlice(value)
}

// appendUniqueString forwards to [payloadcore.AppendUniqueString].
func appendUniqueString(values []string, value string) []string {
	return payloadcore.AppendUniqueString(values, value)
}

func cloneStringSliceMap(values map[string][]string) map[string][]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string][]string, len(values))
	for key, slice := range values {
		cloned[key] = append([]string(nil), slice...)
	}
	return cloned
}
