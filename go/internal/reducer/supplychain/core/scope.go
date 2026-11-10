// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

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
	if _, _, ok := findingMatchableID(s.Scope.CVEID, finding); ok {
		return true
	}
	if _, _, ok := findingMatchableID(s.Scope.AdvisoryID, finding); ok {
		return true
	}
	if s.Scope.PackageID != "" && packageAnchorEqual(s.Scope.PackageID, finding.PackageID) {
		return true
	}
	if s.Scope.PURL != "" && packageAnchorEqual(s.Scope.PURL, finding.PURL) {
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
	// A scoped id matches through the finding's matchable set (#10409 D1):
	// advisory-group member ids plus cve_id and advisory_id. Either scope
	// field consults the whole set because the canonical/member kind split
	// no longer holds after grouping (#10407), and VEX producers do not
	// agree on which field a non-CVE id belongs in. Both fields stay
	// conjunctive: every populated identity field must name this finding.
	if s.Scope.CVEID != "" {
		if _, _, ok := findingMatchableID(s.Scope.CVEID, finding); !ok {
			return false
		}
	}
	if s.Scope.AdvisoryID != "" {
		if _, _, ok := findingMatchableID(s.Scope.AdvisoryID, finding); !ok {
			return false
		}
	}
	if !scopePackageAnchorMatches(s.Scope.PackageID, finding.PackageID) {
		return false
	}
	if !scopePackageAnchorMatches(s.Scope.PURL, finding.PURL) {
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
	// (runtime.go). Environment has NO shared join key
	// to WorkloadID/ServiceID at all (a separate fact source,
	// reducer_ci_cd_run_correlation, correlated only by repository_id), so a
	// scope combining Environment with either one can only be verified when
	// every referenced dimension is single-valued (see that function's doc).
	// WorkloadID and ServiceID DO share a
	// genuine join: supplychainmodel.ServiceContext carries both together from
	// the SAME reducer_service_catalog_correlation record, and
	// applySupplyChainRuntimeContext preserves that
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
	if !suppressionWorkloadScopeMatches(s.Scope, finding.WorkloadIDs) {
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
//   - WorkloadID+ServiceID: supplychainmodel.ServiceContext
//     (supplychainmodel/types.go) carries ServiceID and WorkloadID together
//     on the SAME row --
//     both fields come from the SAME reducer_service_catalog_correlation
//     fact. That is real correlation, so when the scope references BOTH,
//     this delegates to suppressionServiceWorkloadPairMatches, which checks
//     finding.ServiceWorkloadPairs for a genuine co-occurrence instead of
//     the cardinality heuristic below (see that function's doc for why the
//     heuristic is unsound for this pair specifically).
//   - Environment+either: Environment is populated from a DIFFERENT fact
//     kind entirely (reducer_ci_cd_run_correlation via
//     supplychainmodel.DeploymentContext), correlated to the finding only by
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
// (dropped as empty by payloadcore.UniqueSortedStrings), plus an UNRELATED workload
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

// findingMatchableID reports whether a scoped identity value names the
// finding through its matchable set (#10409 D1): advisory-group member ids
// plus the finding's cve_id and advisory_id. On a match it returns the
// finding-side id that matched and whether the match came through the group
// member set rather than a direct cve_id/advisory_id naming, so the decision
// can record audit provenance. An empty scoped value never matches (callers
// treat it as a wildcard separately); an ungrouped finding's set is exactly
// {cve_id, advisory_id}, so fallback findings never match through a partial
// component. Upstream and related ids never enter the set.
func findingMatchableID(scoped string, finding SupplyChainImpactFinding) (matched string, viaGroup bool, ok bool) {
	scoped = strings.TrimSpace(scoped)
	if scoped == "" {
		return "", false, false
	}
	if id := strings.TrimSpace(finding.CVEID); id != "" && strings.EqualFold(scoped, id) {
		return id, false, true
	}
	if id := strings.TrimSpace(finding.AdvisoryID); id != "" && strings.EqualFold(scoped, id) {
		return id, false, true
	}
	for _, member := range finding.AdvisoryGroupMembers {
		if id := strings.TrimSpace(member); id != "" && strings.EqualFold(scoped, id) {
			return id, true, true
		}
	}
	return "", false, false
}

// scopePackageAnchorMatches is scopeAnchorMatches for a package_id or purl
// anchor. Package identity folds ASCII letters only (#9584): strings.EqualFold
// treats U+212A (Kelvin sign) as k and U+017F (long s) as s, which let a
// suppression written for a real package hide findings on its lookalike.
func scopePackageAnchorMatches(scoped, observed string) bool {
	scoped = strings.TrimSpace(scoped)
	if scoped == "" {
		return true
	}
	return packageAnchorEqual(scoped, strings.TrimSpace(observed))
}

// packageAnchorEqual compares two package IDs or PURLs ignoring ASCII case
// only. CVE and advisory IDs stay on strings.EqualFold: they are ASCII by
// specification.
func packageAnchorEqual(left, right string) bool {
	return packageidentity.LowerASCII(left) == packageidentity.LowerASCII(right)
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

// suppressionIndex is the per-pass candidate index (#10409 D2). It maps
// case-folded discoverable anchor values to the suppressions naming them, so
// a finding evaluates only suppressions that can be adjacent to it instead
// of rescanning every suppression: cost is O(S + F·M) lookups for F findings
// with at most M matchable ids. The index is a pure prefilter —
// EvaluateSupplyChainSuppression still rechecks adjacency and scope on every
// candidate — so it must yield a SUPERSET of the suppressions
// suppressionAdjacent would accept. Suppressions whose anchors cannot be
// keyed exactly (no discoverable anchor, a whitespace-only anchor, or a
// non-ASCII anchor value where EqualFold admits folds a map key cannot
// reproduce, e.g. the Kelvin sign) ride the always list and are candidates
// for every finding, exactly as the full scan treated them. A finding with
// a whitespace-only or non-ASCII anchor or member id likewise falls back
// to the full set.
type suppressionIndex struct {
	all        []vulnerabilitySuppression
	byIdentity map[string][]int
	byPackage  map[string][]int
	byPURL     map[string][]int
	byRepo     map[string][]int
	byDigest   map[string][]int
	always     []int
}

// suppressionFoldKey folds an identity or anchor value for index lookup.
// CVE/advisory ids are ASCII by specification and repository/digest anchors
// are compared with EqualFold; for ASCII values ToUpper equality is exactly
// EqualFold equality. Non-ASCII values never reach the maps (see
// suppressionIndex).
func suppressionFoldKey(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func suppressionASCIIFoldable(value string) bool {
	for _, r := range value {
		if r > 127 {
			return false
		}
	}
	return true
}

func newSuppressionIndex(suppressions []vulnerabilitySuppression) suppressionIndex {
	index := suppressionIndex{
		all:        suppressions,
		byIdentity: make(map[string][]int, len(suppressions)),
		byPackage:  make(map[string][]int),
		byPURL:     make(map[string][]int),
		byRepo:     make(map[string][]int),
		byDigest:   make(map[string][]int),
	}
	add := func(m map[string][]int, key string, i int) {
		if key != "" {
			m[key] = append(m[key], i)
		}
	}
	for i := range suppressions {
		scope := suppressions[i].Scope
		anchors := []string{scope.CVEID, scope.AdvisoryID, scope.PackageID, scope.PURL, scope.RepositoryID, scope.SubjectDigest}
		foldable := suppressionScopeHasDiscoverableAnchor(scope)
		for _, anchor := range anchors {
			// A whitespace-only anchor adjacent-matches an equally padded
			// finding anchor in the full scan (untrimmed comparisons) but
			// keys to nothing here, so it rides the always list too.
			if anchor != "" && (strings.TrimSpace(anchor) == "" || !suppressionASCIIFoldable(anchor)) {
				foldable = false
			}
		}
		if !foldable {
			index.always = append(index.always, i)
			continue
		}
		add(index.byIdentity, suppressionFoldKey(scope.CVEID), i)
		add(index.byIdentity, suppressionFoldKey(scope.AdvisoryID), i)
		add(index.byPackage, packageidentity.LowerASCII(strings.TrimSpace(scope.PackageID)), i)
		add(index.byPURL, packageidentity.LowerASCII(strings.TrimSpace(scope.PURL)), i)
		add(index.byRepo, suppressionFoldKey(scope.RepositoryID), i)
		add(index.byDigest, suppressionFoldKey(scope.SubjectDigest), i)
	}
	return index
}

// candidates returns every suppression that can be adjacent to finding, in
// the original input order so selection ties break exactly as the full scan.
func (index suppressionIndex) candidates(finding SupplyChainImpactFinding) []vulnerabilitySuppression {
	if len(index.all) == 0 {
		return nil
	}
	identityIDs := make([]string, 0, len(finding.AdvisoryGroupMembers)+2)
	identityIDs = append(identityIDs, finding.CVEID, finding.AdvisoryID)
	identityIDs = append(identityIDs, finding.AdvisoryGroupMembers...)
	probe := append(append([]string{}, identityIDs...), finding.PackageID, finding.PURL, finding.RepositoryID, finding.SubjectDigest)
	for _, value := range probe {
		if value != "" && (strings.TrimSpace(value) == "" || !suppressionASCIIFoldable(value)) {
			return index.all
		}
	}
	seen := make(map[int]struct{}, 16)
	pick := make([]int, 0, 16)
	take := func(idxs []int) {
		for _, idx := range idxs {
			if _, ok := seen[idx]; !ok {
				seen[idx] = struct{}{}
				pick = append(pick, idx)
			}
		}
	}
	for _, id := range identityIDs {
		take(index.byIdentity[suppressionFoldKey(id)])
	}
	take(index.byPackage[packageidentity.LowerASCII(strings.TrimSpace(finding.PackageID))])
	take(index.byPURL[packageidentity.LowerASCII(strings.TrimSpace(finding.PURL))])
	take(index.byRepo[suppressionFoldKey(finding.RepositoryID)])
	take(index.byDigest[suppressionFoldKey(finding.SubjectDigest)])
	take(index.always)
	sort.Ints(pick)
	out := make([]vulnerabilitySuppression, 0, len(pick))
	for _, idx := range pick {
		out = append(out, index.all[idx])
	}
	return out
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
