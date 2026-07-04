// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Skip reason labels for the CAN_PERFORM skipped counter. They are the bounded
// skip_reason metric dimension members for eshu_dp_iam_can_perform_skipped_total
// and the completion-log breakdown (design §3 skip taxonomy). Each names a
// distinct conservative refusal so an operator can tell why CAN_PERFORM edges are
// missing rather than guessing. A grant is never dropped silently.
const (
	iamCanPerformSkipUncatalogued       = "skipped_uncatalogued_action"
	iamCanPerformSkipAmbiguous          = "skipped_ambiguous"
	iamCanPerformSkipUnresolved         = "skipped_unresolved"
	iamCanPerformSkipDeny               = "skipped_deny"
	iamCanPerformSkipConditioned        = "skipped_conditioned"
	iamCanPerformSkipNotActionResource  = "skipped_not_action_resource"
	iamCanPerformSkipSelfLoop           = "skipped_self_loop"
	iamCanPerformSkipPermissionBoundary = "skipped_permission_boundary"
)

const iamCanPerformConditionConfidenceProvenanceOnly = "provenance_only"

// Resolution modes for the CAN_PERFORM edges counter. They are the bounded
// resolution_mode metric dimension members for eshu_dp_iam_can_perform_edges_total
// — the two confident ways a catalog action's resource ARN resolves to exactly one
// scanned node (design §3.2). Ambiguous / wildcard / zero do not produce an edge
// and are counted under the skipped counter instead.
const (
	iamCanPerformResolutionExactARN   = "exact_arn"
	iamCanPerformResolutionSingleGlob = "single_glob"
)

// iamCanPerformTally is the honest accounting surface for catalog-action
// evaluations that did not become an edge. Each counter names a conservative
// refusal reason; a grant is never dropped silently (design §3 skip rules).
type iamCanPerformTally struct {
	skippedUncatalogued       int
	skippedAmbiguous          int
	skippedUnresolved         int
	skippedDeny               int
	skippedConditioned        int
	conditionedProvenanceOnly int
	skippedNotActionResource  int
	skippedSelfLoop           int
	skippedPermissionBoundary int
}

// total returns the count of evaluations that produced no edge.
func (t iamCanPerformTally) total() int {
	return t.skippedUncatalogued + t.skippedAmbiguous + t.skippedUnresolved +
		t.skippedDeny + t.skippedConditioned + t.skippedNotActionResource +
		t.skippedSelfLoop + t.skippedPermissionBoundary
}

// IAMCanPerformResult is the bounded, deterministic output of one generation's
// CAN_PERFORM extraction: the edge rows to upsert (one per resolved
// principal->resource pair, carrying the merged sorted granted action set), the
// per-edge resolution mode for the edges counter, and the skip tally. Edge rows
// are deduplicated by (principal_uid, resource_uid) — when several catalog actions
// resolve to the same resource node they merge into one row's sorted actions list
// — and sorted for byte-stable batched writes.
type IAMCanPerformResult struct {
	Edges []map[string]any
	// EdgesByMode counts emitted edges keyed by resolution_mode so the handler can
	// record eshu_dp_iam_can_perform_edges_total{resolution_mode}. An edge that
	// merged an exact-ARN action and a single-glob action is counted under the
	// strongest (exact_arn) mode so the per-edge label is deterministic.
	EdgesByMode map[string]int
	Tally       iamCanPerformTally
	// Quarantined carries the facts skipped as input_invalid during decode (a
	// missing required identity field), so the handler emits a visible per-fact
	// dead-letter while the valid facts still project.
	Quarantined []quarantinedFact
}

// iamCanPerformEdgeAccumulator collects, per (principal, resource) edge identity,
// the merged granted action set and the strongest resolution mode seen, so two
// catalog actions reaching the same resource converge on one idempotent edge.
type iamCanPerformEdgeAccumulator struct {
	actions                     map[string]struct{}
	sources                     map[string]struct{}
	mode                        string
	permissionBoundaryEvaluated bool
}

// ExtractIAMCanPerformEdges resolves each scanned IAM principal's trusted-Allow
// identity statements against the closed CAN_PERFORM catalog and emits a
// CAN_PERFORM edge for every (principal, resource) pair where a catalog action is
// granted (Allow, unconditioned, no NotAction/NotResource, not Deny-touched) AND
// the action's resource ARN resolves to EXACTLY ONE scanned CloudResource node of
// the catalog-expected type, and any attached permissions boundary also allows
// that action/resource. It also evaluates resource-policy facts when the grantee
// is an exact scanned IAM role/user and the statement Resource applies to the
// attached resource. Wildcard / many / zero / public or unscanned principal /
// Deny / conditioned / NotAction / uncatalogued / boundary-missing-allow /
// self-loop all degrade to a counted skip, never an edge, and it never fabricates
// a node (graceful degradation).
//
// Returned edge rows are deduplicated by (principal_uid, resource_uid) with merged
// sorted actions and sorted deterministically so the batched write is stable
// across retries and reprojections (idempotent). The honesty boundary is encoded
// by the rel.grant_sources and rel.evaluation_scope properties the writer stamps.
func ExtractIAMCanPerformEdges(
	resourceEnvelopes []facts.Envelope,
	permissionEnvelopes []facts.Envelope,
	resourcePolicyEnvelopeSets ...[]facts.Envelope,
) (IAMCanPerformResult, error) {
	result := IAMCanPerformResult{EdgesByMode: make(map[string]int)}
	resourcePolicyEnvelopes := flattenResourcePolicyEnvelopeSets(resourcePolicyEnvelopeSets)
	if len(permissionEnvelopes) == 0 && len(resourcePolicyEnvelopes) == 0 {
		return result, nil
	}

	index, resourceQuarantined, err := buildCloudResourceJoinIndex(resourceEnvelopes)
	if err != nil {
		return IAMCanPerformResult{EdgesByMode: make(map[string]int)}, err
	}
	result.Quarantined = append(result.Quarantined, resourceQuarantined...)
	principals, principalQuarantined, err := groupIAMCanPerformByPrincipal(index, permissionEnvelopes, &result.Tally)
	if err != nil {
		return IAMCanPerformResult{EdgesByMode: make(map[string]int)}, err
	}
	result.Quarantined = append(result.Quarantined, principalQuarantined...)
	boundariesByPrincipal, boundaryQuarantined, err := groupIAMCanPerformBoundaryEvidence(index, permissionEnvelopes)
	if err != nil {
		return IAMCanPerformResult{EdgesByMode: make(map[string]int)}, err
	}
	result.Quarantined = append(result.Quarantined, boundaryQuarantined...)
	catalog := iamCanPerformCatalogByAction()

	// edge identity -> merged granted action set + strongest resolution mode, so
	// several catalog actions to the same resource converge on one idempotent edge.
	edges := make(map[edgeKey]*iamCanPerformEdgeAccumulator)

	for _, principal := range principals {
		grant := buildIAMCanPerformGrant(principal.permissions, &result.Tally)

		for _, entry := range catalog {
			switch {
			case grant.denied(entry.Action):
				result.Tally.skippedDeny++
				continue
			case !grant.allows(entry.Action):
				// The principal simply does not hold this catalog action. Not a skip:
				// "principal lacks the action" is the common, expected case and would
				// drown the skip signal. (Conditioned / NotAction carriers of a catalog
				// action are already counted in buildIAMCanPerformGrant.)
				continue
			}

			resourceUID, mode, status := resolveIAMCanPerformTarget(index, grant, entry)
			switch status {
			case iamTargetResolved:
				if resourceUID == principal.principalUID {
					// A self-referential grant (principal ARN == resource ARN) carries no
					// cross-node CAN_PERFORM truth; count it so the refusal is visible.
					result.Tally.skippedSelfLoop++
					continue
				}
				resourceARN, ok := index.arnForUID(resourceUID)
				if !ok {
					result.Tally.skippedUnresolved++
					continue
				}
				boundaryDecision := evaluateIAMCanPerformPermissionBoundary(
					boundariesByPrincipal[principal.principalUID],
					entry.Action,
					resourceARN,
					entry.ExpectedResourceType,
				)
				if boundaryDecision.skipReason != "" {
					result.Tally.recordSkip(boundaryDecision.skipReason)
					continue
				}
				addIAMCanPerformEdge(
					edges,
					principal.principalUID,
					resourceUID,
					entry.Action,
					mode,
					iamCanPerformGrantSourceIdentityPolicy,
					boundaryDecision.evaluated,
				)
			case iamTargetAmbiguous:
				result.Tally.skippedAmbiguous++
			default:
				result.Tally.skippedUnresolved++
			}
		}
	}

	resourcePolicyQuarantined, err := addIAMCanPerformResourcePolicyEdges(index, resourcePolicyEnvelopes, catalog, edges, &result.Tally)
	if err != nil {
		return IAMCanPerformResult{EdgesByMode: make(map[string]int)}, err
	}
	result.Quarantined = append(result.Quarantined, resourcePolicyQuarantined...)
	result.Edges = buildIAMCanPerformEdgeRows(edges, result.EdgesByMode)
	return result, nil
}

// iamCanPerformActionIsCatalogued reports whether one granted action covers at
// least one catalog action, matching iamPrincipalGrant.allows precedence: the
// admin wildcard "*" and a service wildcard "service:*" whose service holds a
// catalog action are catalogued; a concrete action is catalogued only by exact
// membership. A concrete action whose service has catalog entries but whose verb
// is not catalogued (e.g. s3:listbucket when only s3:getobject is catalogued) is
// NOT catalogued — that is the closed-vocabulary boundary.
func iamCanPerformActionIsCatalogued(action string, catalogActions map[string]struct{}) bool {
	if action == "*" {
		return true
	}
	if _, ok := catalogActions[action]; ok {
		return true
	}
	if service, verb, ok := strings.Cut(action, ":"); ok && verb == "*" && service != "" {
		for catalogAction := range catalogActions {
			if cs, _, ok := strings.Cut(catalogAction, ":"); ok && cs == service {
				return true
			}
		}
	}
	return false
}

// groupIAMCanPerformByPrincipal buckets permission facts by principal_arn and
// resolves each principal's own CloudResource node uid. A principal that was not
// scanned has no source node to anchor an edge on, so its statements are dropped
// and counted skippedUnresolved (one count per principal). The returned slice is
// sorted by principal uid for deterministic iteration.
func groupIAMCanPerformByPrincipal(
	index cloudResourceJoinIndex,
	permissionEnvelopes []facts.Envelope,
	tally *iamCanPerformTally,
) ([]iamPrincipalStatements, []quarantinedFact, error) {
	byPrincipalARN := make(map[string][]iamPermissionStatement)
	order := make([]string, 0)
	var quarantined []quarantinedFact
	for _, env := range permissionEnvelopes {
		if env.FactKind != facts.AWSIAMPermissionFactKind || env.IsTombstone {
			continue
		}
		permission, err := decodeAWSIAMPermission(env)
		if err != nil {
			q, ok, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return nil, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
			continue
		}
		if !iamCanPerformIdentityPolicySource(permission.PolicySource) {
			continue
		}
		if permission.PrincipalARN == "" {
			continue
		}
		if _, seen := byPrincipalARN[permission.PrincipalARN]; !seen {
			order = append(order, permission.PrincipalARN)
		}
		byPrincipalARN[permission.PrincipalARN] = append(byPrincipalARN[permission.PrincipalARN], iamPermissionStatement{factID: env.FactID, permission: permission})
	}

	principals := make([]iamPrincipalStatements, 0, len(order))
	for _, principalARN := range order {
		uid, ok := index.byARN[principalARN]
		if !ok {
			// The principal itself was not scanned: no anchor node exists, so none of
			// its catalog actions can become an edge. Count once and skip the principal.
			tally.skippedUnresolved++
			continue
		}
		principals = append(principals, iamPrincipalStatements{principalUID: uid, permissions: byPrincipalARN[principalARN]})
	}
	sort.Slice(principals, func(a, b int) bool {
		return principals[a].principalUID < principals[b].principalUID
	})
	return principals, quarantined, nil
}

func addIAMCanPerformEdge(
	edges map[edgeKey]*iamCanPerformEdgeAccumulator,
	principalUID string,
	resourceUID string,
	action string,
	mode string,
	source string,
	permissionBoundaryEvaluated bool,
) {
	key := edgeKey{principalUID: principalUID, targetUID: resourceUID}
	acc := edges[key]
	if acc == nil {
		acc = &iamCanPerformEdgeAccumulator{
			actions: make(map[string]struct{}),
			sources: make(map[string]struct{}),
		}
		edges[key] = acc
	}
	acc.actions[action] = struct{}{}
	acc.sources[source] = struct{}{}
	acc.mode = strongestCanPerformMode(acc.mode, mode)
	acc.permissionBoundaryEvaluated = acc.permissionBoundaryEvaluated || permissionBoundaryEvaluated
}

// buildIAMCanPerformEdgeRows turns the merged per-edge accumulators into sorted,
// byte-stable edge rows. Each row carries the merged sorted granted action set,
// an action_count for cheap operator filtering, and the evaluation_scope honesty
// label. Rows are sorted by (principal_uid, resource_uid) so the batched write is
// deterministic; the edge resolution mode is tallied into modeCounts for the
// edges counter.
func buildIAMCanPerformEdgeRows(
	edges map[edgeKey]*iamCanPerformEdgeAccumulator,
	modeCounts map[string]int,
) []map[string]any {
	if len(edges) == 0 {
		return nil
	}
	rows := make([]map[string]any, 0, len(edges))
	for key, acc := range edges {
		actions := sortedCanPerformActions(acc.actions)
		grantSources := sortedCanPerformStringSet(acc.sources)
		modeCounts[acc.mode]++
		rows = append(rows, map[string]any{
			"principal_uid":    key.principalUID,
			"resource_uid":     key.targetUID,
			"actions":          actions,
			"action_count":     len(actions),
			"evaluation_scope": iamCanPerformEvaluationScopeForSources(grantSources, acc.permissionBoundaryEvaluated),
			"grant_sources":    grantSources,
		})
	}
	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["principal_uid"]) + "->" + anyToString(rows[a]["resource_uid"])
		right := anyToString(rows[b]["principal_uid"]) + "->" + anyToString(rows[b]["resource_uid"])
		return left < right
	})
	return rows
}

func iamCanPerformEvaluationScopeForSources(sources []string, permissionBoundaryEvaluated bool) string {
	hasIdentity := false
	hasResource := false
	for _, source := range sources {
		switch source {
		case iamCanPerformGrantSourceIdentityPolicy:
			hasIdentity = true
		case iamCanPerformGrantSourceResourcePolicy:
			hasResource = true
		}
	}
	switch {
	case hasIdentity && hasResource && permissionBoundaryEvaluated:
		return iamCanPerformEvaluationScopeIdentityAndResourcePolicyWithPermissionBoundary
	case hasIdentity && hasResource:
		return iamCanPerformEvaluationScopeIdentityAndResourcePolicy
	case hasIdentity && permissionBoundaryEvaluated:
		return iamCanPerformEvaluationScopeIdentityPolicyWithPermissionBoundary
	case hasResource:
		return iamCanPerformEvaluationScopeResourcePolicyOnly
	default:
		return iamCanPerformEvaluationScopeIdentityPolicyOnly
	}
}

// strongestCanPerformMode keeps the most confident resolution mode for an edge
// that merged several catalog actions: an exact-ARN match outranks a single-glob
// match. The result is deterministic so the per-edge resolution_mode label is
// stable across retries.
func strongestCanPerformMode(existing, candidate string) string {
	if existing == iamCanPerformResolutionExactARN || candidate == iamCanPerformResolutionExactARN {
		return iamCanPerformResolutionExactARN
	}
	return iamCanPerformResolutionSingleGlob
}
