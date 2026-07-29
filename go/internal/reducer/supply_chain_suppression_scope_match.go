// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

// suppressionAdjacent reports whether a suppression names at least one
// discoverable identity anchor the finding also has, so we can tell "could
// this suppression apply to this vulnerability at all?" from "the identity
// applies but narrowing scope did not line up." Deployment fields never make
// different vulnerability identities adjacent. A scope without a discoverable
// identity anchor is treated as adjacent so the invalid suppression is
// preserved for audit, but suppressionScopeMatchesFinding rejects it so it
// never hides a finding.
func suppressionAdjacent(finding SupplyChainImpactFinding, s vulnerabilitySuppression) bool {
	if !suppressionScopeHasDiscoverableAnchor(s.Scope) {
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
	if !suppressionScopeHasDiscoverableAnchor(s.Scope) {
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
	// #5466 round-7 review P1-A, tightened by round-8 review F-2:
	// Environment, WorkloadID, and ServiceID are flattened onto the finding
	// as evidence lists built by applySupplyChainRuntimeContext
	// (supply_chain_impact_runtime.go). Environment has NO shared join key
	// to WorkloadID/ServiceID at all (a separate fact source,
	// reducer_ci_cd_run_correlation, correlated only by repository_id), so a
	// scope combining Environment with either one can only be verified when
	// every referenced dimension is single-valued (see that function's doc).
	// WorkloadID and ServiceID DO share a
	// genuine join: supplyChainServiceContext (supply_chain_impact_index.go)
	// carries both together from the SAME reducer_service_catalog_
	// correlation record, and applySupplyChainRuntimeContext preserves that
	// exact pairing in finding.ServiceWorkloadPairs -- so a scope naming
	// BOTH is verified against a real pair instead of the (unsound, see
	// suppressionServiceWorkloadPairMatches) cardinality heuristic.
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
// deployment evidence is precise enough to apply a scoped decision to the
// whole canonical finding. Every referenced dimension must be single-valued;
// WorkloadID+ServiceID must additionally have a verified pair.
//
// The two remaining dimension-pairs are verified by GENUINELY DIFFERENT
// mechanisms, because only one of them has real correlating evidence:
//
//   - WorkloadID+ServiceID: supplyChainServiceContext (supply_chain_impact_
//     index.go) carries serviceID and workloadID together on the SAME row --
//     both fields come from the SAME reducer_service_catalog_correlation
//     fact. That is real correlation, so when the scope references BOTH,
//     this delegates to suppressionServiceWorkloadPairMatches, which checks
//     finding.ServiceWorkloadPairs for a genuine co-occurrence instead of
//     the cardinality heuristic below (see that function's doc for why the
//     heuristic is unsound for this pair specifically).
//   - Environment+either: Environment is populated from a DIFFERENT fact
//     kind entirely (reducer_ci_cd_run_correlation via
//     supplyChainDeploymentContext), correlated to the finding only by
//     repository_id -- there is no row anywhere that carries Environment
//     alongside WorkloadID or ServiceID, so it cannot be tupled with either
//     the way WorkloadID and ServiceID can be tupled with each other. A
//     scope combining Environment with one of them therefore relies on the
//     singleton guard below. With at most one candidate per referenced
//     dimension, independent checks describe only one possible combination.
//
// A referenced dimension must itself be single-valued. The finding carries
// one suppression decision for the whole canonical aggregate, so matching one
// value in a multi-valued dimension would hide every other context too.
func suppressionDeploymentContextUnambiguous(finding SupplyChainImpactFinding, scope vulnerabilitySuppressionScope) bool {
	env := strings.TrimSpace(scope.Environment) != ""
	workload := strings.TrimSpace(scope.WorkloadID) != ""
	service := strings.TrimSpace(scope.ServiceID) != ""
	if env && !suppressionDimensionSingleValued(finding.Environments) {
		return false
	}
	if workload && !suppressionDimensionSingleValued(finding.WorkloadIDs) {
		return false
	}
	if service && !suppressionDimensionSingleValued(finding.ServiceIDs) {
		return false
	}
	if workload && service {
		return suppressionServiceWorkloadPairMatches(finding, scope)
	}
	return true
}

// suppressionServiceWorkloadPairMatches reports whether finding has at least
// one ServiceWorkloadPairs entry whose ServiceID and WorkloadID both
// case-insensitively equal scope's (#5466 round-8 review F-2). This is NOT
// equivalent to two independent scopeListAnchorMatches calls against
// finding.ServiceIDs/WorkloadIDs: those lists are flattened from multiple
// sources, and WorkloadIDs in particular mixes reducer_service_catalog_
// correlation's genuinely-paired workload IDs with reducer_workload_
// identity's workload IDs, which have no known service at all. A service
// record that resolved ServiceID="service-x" but not its WorkloadID
// (dropped as empty by uniqueSortedStrings), plus an UNRELATED workload
// record contributing "workload-b", would each independently satisfy
// scopeListAnchorMatches for service_id=service-x and workload_id=
// workload-b even though that combination never occurred anywhere -- the
// exact over-suppression the round-7 P1-A guard could not close, because
// its cardinality check saw both lists at cardinality 1 and declared them
// unambiguous. An empty finding.ServiceWorkloadPairs (no service-catalog
// evidence at all) fails closed to no-match, same "ambiguity resolves to
// visible" direction as every other #5466 fail-closed check.
func suppressionServiceWorkloadPairMatches(finding SupplyChainImpactFinding, scope vulnerabilitySuppressionScope) bool {
	scopedService := strings.TrimSpace(scope.ServiceID)
	scopedWorkload := strings.TrimSpace(scope.WorkloadID)
	for _, pair := range finding.ServiceWorkloadPairs {
		if strings.EqualFold(strings.TrimSpace(pair.ServiceID), scopedService) &&
			strings.EqualFold(strings.TrimSpace(pair.WorkloadID), scopedWorkload) {
			return true
		}
	}
	return false
}

// suppressionDimensionSingleValued reports whether values contain at most one
// distinct trimmed, non-empty value without allocating a set on the
// per-finding suppression path. Exact comparison is conservative: case
// variants remain ambiguous and therefore visible.
func suppressionDimensionSingleValued(values []string) bool {
	var first string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if first == "" {
			first = value
			continue
		}
		if value != first {
			return false
		}
	}
	return true
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

// suppressionScopeHasDiscoverableAnchor reports whether storage can discover
// the suppression from a finding identity. Deployment context and evidence
// path are narrowing-only: accepting any of them as a sole anchor would turn
// an environment, workload, or service into a cross-vulnerability wildcard.
func suppressionScopeHasDiscoverableAnchor(scope vulnerabilitySuppressionScope) bool {
	return strings.TrimSpace(scope.CVEID) != "" ||
		strings.TrimSpace(scope.AdvisoryID) != "" ||
		strings.TrimSpace(scope.PackageID) != "" ||
		strings.TrimSpace(scope.PURL) != "" ||
		strings.TrimSpace(scope.RepositoryID) != "" ||
		strings.TrimSpace(scope.SubjectDigest) != ""
}
