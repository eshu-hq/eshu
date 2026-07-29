// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

// suppressionAdjacent reports whether a suppression names at least one anchor
// the finding also has, so we can tell "could this suppression apply to this
// finding's identity at all?" from "applies but scope did not line up." An
// empty scope is still treated as adjacent so the suppression is preserved on
// every finding decision for audit, but suppressionScopeMatchesFinding
// rejects empty scope so it never silently hides a finding.
func suppressionAdjacent(finding SupplyChainImpactFinding, s vulnerabilitySuppression) bool {
	if suppressionScopeIsEmpty(s.Scope) {
		return true
	}
	if s.Scope.CVEID != "" && strings.EqualFold(s.Scope.CVEID, finding.CVEID) {
		return true
	}
	if s.Scope.AdvisoryID != "" && strings.EqualFold(s.Scope.AdvisoryID, finding.AdvisoryID) {
		return true
	}
	if s.Scope.PackageID != "" && strings.EqualFold(s.Scope.PackageID, finding.PackageID) {
		return true
	}
	if s.Scope.PURL != "" && strings.EqualFold(s.Scope.PURL, finding.PURL) {
		return true
	}
	if s.Scope.RepositoryID != "" && strings.EqualFold(s.Scope.RepositoryID, finding.RepositoryID) {
		return true
	}
	if s.Scope.SubjectDigest != "" && strings.EqualFold(s.Scope.SubjectDigest, finding.SubjectDigest) {
		return true
	}
	if s.Scope.Environment != "" && scopeListAnchorMatches(s.Scope.Environment, finding.Environments) {
		return true
	}
	if s.Scope.WorkloadID != "" && scopeListAnchorMatches(s.Scope.WorkloadID, finding.WorkloadIDs) {
		return true
	}
	if s.Scope.ServiceID != "" && scopeListAnchorMatches(s.Scope.ServiceID, finding.ServiceIDs) {
		return true
	}
	return false
}

// suppressionScopeMatchesFinding returns true only when every populated scope
// key matches the finding. Empty scope keys act as wildcards within an
// otherwise-bounded scope, but a scope that names nothing at all is treated
// as a mismatch so a malformed or missing scope payload can never silently
// hide every finding (the suppression still surfaces as scope_mismatch for
// audit). Evidence path entries must all appear in the finding's evidence
// path.
func suppressionScopeMatchesFinding(finding SupplyChainImpactFinding, s vulnerabilitySuppression) bool {
	if suppressionScopeIsEmpty(s.Scope) {
		return false
	}
	if !scopeAnchorMatches(s.Scope.CVEID, finding.CVEID) {
		return false
	}
	if !scopeAnchorMatches(s.Scope.AdvisoryID, finding.AdvisoryID) {
		return false
	}
	if !scopeAnchorMatches(s.Scope.PackageID, finding.PackageID) {
		return false
	}
	if !scopeAnchorMatches(s.Scope.PURL, finding.PURL) {
		return false
	}
	if !scopeAnchorMatches(s.Scope.RepositoryID, finding.RepositoryID) {
		return false
	}
	if !scopeAnchorMatches(s.Scope.SubjectDigest, finding.SubjectDigest) {
		return false
	}
	// #5466 round-7 review P1-A (codex): Environment, WorkloadID, and
	// ServiceID are flattened onto the finding as three INDEPENDENTLY
	// matched, uncorrelated evidence lists (applySupplyChainRuntimeContext,
	// supply_chain_impact_runtime.go, matches supplyChainDeploymentContext/
	// supplyChainWorkloadContext/supplyChainServiceContext each against the
	// finding's repository/digest/image-ref -- none of those three structs
	// share a field with each other beyond repository_id; see
	// supply_chain_impact_index.go). So a finding whose evidence aggregates
	// TWO deployments, say (stage, workload-a) and (prod, workload-b), has
	// Environments=[prod,stage] and WorkloadIDs=[workload-a,workload-b] with
	// no record of which environment paired with which workload. Checking
	// each anchor independently against its own list (scopeListAnchorMatches
	// below) would let a scope of environment=stage + workload_id=workload-b
	// match, even though that exact combination never occurred in any real
	// deployment -- the fail-closed rule exists precisely to prevent this
	// kind of over-suppression. When the scope names two or more of these
	// three dimensions, suppressionDeploymentContextUnambiguous requires the
	// finding to have AT MOST ONE distinct value in every dimension the
	// scope references, so the independent per-list checks below are
	// equivalent to verifying a single, unambiguous deployment context
	// satisfies the whole combination. A finding with two or more distinct
	// values in any referenced dimension cannot be verified this way and
	// fails closed to no-match (same "ambiguity resolves to visible"
	// direction as the empty-list case). A scope naming zero or one of these
	// three dimensions has no combination to verify and is unaffected.
	if !suppressionDeploymentContextUnambiguous(finding, s.Scope) {
		return false
	}
	// Environment/WorkloadID/ServiceID match against the finding's
	// multi-value evidence lists rather than a single field: a finding can
	// carry more than one deployment's environment/workload/service. An
	// empty scope value is a wildcard (scopeListAnchorMatches short-circuits
	// true); a non-empty scope value against an empty finding list fails
	// closed to no-match so a suppression can never hide a finding it has no
	// evidence for (#5466 fail-closed rule -- ambiguity resolves to visible).
	if !scopeListAnchorMatches(s.Scope.Environment, finding.Environments) {
		return false
	}
	if !scopeListAnchorMatches(s.Scope.WorkloadID, finding.WorkloadIDs) {
		return false
	}
	if !scopeListAnchorMatches(s.Scope.ServiceID, finding.ServiceIDs) {
		return false
	}
	if !evidencePathContainsAll(finding.EvidencePath, s.Scope.EvidencePath) {
		return false
	}
	return true
}

// suppressionDeploymentContextUnambiguous reports whether the finding's
// deployment evidence is precise enough to verify a scope naming two or more
// of {Environment, WorkloadID, ServiceID} against a SINGLE deployment
// context (#5466 round-7 review P1-A). These three dimensions are populated
// on the finding from three independently matched fact sources with no
// shared join key besides repository_id (see the call site's comment for
// the full trace), so this reducer has no evidence of which environment
// paired with which workload/service when a finding aggregates more than
// one deployment. When the scope references two or more of the three
// dimensions, this returns true only if EVERY referenced dimension has at
// most one distinct value on the finding: with at most one candidate value
// per referenced dimension there is only one possible combination, so the
// independent per-dimension checks are equivalent to checking that single
// combination. A dimension with two or more distinct values makes the
// combination unverifiable and this returns false (fail closed -- the
// suppression does not apply, the finding stays visible). A scope
// referencing zero or one of the three dimensions always returns true: with
// nothing to combine there is no cross-dimension ambiguity to guard
// against, and behavior is unchanged from before this guard existed.
func suppressionDeploymentContextUnambiguous(finding SupplyChainImpactFinding, scope vulnerabilitySuppressionScope) bool {
	referenced := 0
	if strings.TrimSpace(scope.Environment) != "" {
		referenced++
	}
	if strings.TrimSpace(scope.WorkloadID) != "" {
		referenced++
	}
	if strings.TrimSpace(scope.ServiceID) != "" {
		referenced++
	}
	if referenced < 2 {
		return true
	}
	if strings.TrimSpace(scope.Environment) != "" && distinctNonEmptyValueCount(finding.Environments) > 1 {
		return false
	}
	if strings.TrimSpace(scope.WorkloadID) != "" && distinctNonEmptyValueCount(finding.WorkloadIDs) > 1 {
		return false
	}
	if strings.TrimSpace(scope.ServiceID) != "" && distinctNonEmptyValueCount(finding.ServiceIDs) > 1 {
		return false
	}
	return true
}

// distinctNonEmptyValueCount counts the distinct trimmed, non-empty values
// in values. Exact-string comparison (not case-insensitive) is intentional:
// treating case variants as distinct is the more conservative reading for
// suppressionDeploymentContextUnambiguous's fail-closed check, since it can
// only ever make ambiguity detection MORE likely to fire, never less.
func distinctNonEmptyValueCount(values []string) int {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		seen[value] = struct{}{}
	}
	return len(seen)
}

// scopeListAnchorMatches reports whether a scoped anchor value matches at
// least one element of a finding's multi-value evidence field (Environments,
// WorkloadIDs, or ServiceIDs). An empty scoped value is a wildcard and always
// matches, including a finding with no evidence for that anchor. A non-empty
// scoped value against an empty observed list fails closed to no-match: a
// finding with no evidence for the anchor a suppression names is never
// narrowed into by that suppression (#5466).
func scopeListAnchorMatches(scoped string, observed []string) bool {
	scoped = strings.TrimSpace(scoped)
	if scoped == "" {
		return true
	}
	for _, value := range observed {
		if strings.EqualFold(scoped, strings.TrimSpace(value)) {
			return true
		}
	}
	return false
}

func scopeAnchorMatches(scoped, observed string) bool {
	scoped = strings.TrimSpace(scoped)
	if scoped == "" {
		return true
	}
	return strings.EqualFold(scoped, strings.TrimSpace(observed))
}

func evidencePathContainsAll(observed []string, required []string) bool {
	if len(required) == 0 {
		return true
	}
	have := make(map[string]struct{}, len(observed))
	for _, step := range observed {
		step = strings.TrimSpace(step)
		if step == "" {
			continue
		}
		have[step] = struct{}{}
	}
	for _, step := range required {
		step = strings.TrimSpace(step)
		if step == "" {
			continue
		}
		if _, ok := have[step]; !ok {
			return false
		}
	}
	return true
}

func suppressionScopeIsEmpty(scope vulnerabilitySuppressionScope) bool {
	return strings.TrimSpace(scope.CVEID) == "" &&
		strings.TrimSpace(scope.AdvisoryID) == "" &&
		strings.TrimSpace(scope.PackageID) == "" &&
		strings.TrimSpace(scope.PURL) == "" &&
		strings.TrimSpace(scope.RepositoryID) == "" &&
		strings.TrimSpace(scope.SubjectDigest) == "" &&
		strings.TrimSpace(scope.Environment) == "" &&
		strings.TrimSpace(scope.WorkloadID) == "" &&
		strings.TrimSpace(scope.ServiceID) == "" &&
		len(scope.EvidencePath) == 0
}
