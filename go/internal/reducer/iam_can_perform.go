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
	iamCanPerformSkipUncatalogued      = "skipped_uncatalogued_action"
	iamCanPerformSkipAmbiguous         = "skipped_ambiguous"
	iamCanPerformSkipUnresolved        = "skipped_unresolved"
	iamCanPerformSkipDeny              = "skipped_deny"
	iamCanPerformSkipConditioned       = "skipped_conditioned"
	iamCanPerformSkipNotActionResource = "skipped_not_action_resource"
	iamCanPerformSkipSelfLoop          = "skipped_self_loop"
)

// Permission-boundary skip reasons (PR4c). They extend the bounded skip taxonomy
// with the precise conservative reason a candidate identity-policy edge did NOT
// survive intersection with the principal's permission boundary. A principal with
// NO boundary is unaffected and never lands in these counters. None of the labels
// carry policy values or secret-like strings — they are fixed tokens.
const (
	// iamCanPerformSkipBoundaryNoAllow: the principal has a boundary, but the
	// boundary does not allow this action on a resource covering the resolved
	// target. The identity allow is therefore non-effective.
	iamCanPerformSkipBoundaryNoAllow = "skipped_boundary_no_allow"
	// iamCanPerformSkipBoundaryDeny: the boundary explicitly Denies this action, so
	// the candidate edge is removed regardless of any boundary allow.
	iamCanPerformSkipBoundaryDeny = "skipped_boundary_deny"
	// iamCanPerformSkipBoundaryConditioned: the only boundary statement covering
	// this action is condition-gated, so it cannot be conservatively treated as a
	// permissive boundary.
	iamCanPerformSkipBoundaryConditioned = "skipped_boundary_conditioned"
	// iamCanPerformSkipBoundaryNotActionResource: the only boundary statement
	// covering this action uses NotAction/NotResource, which inverts the match space
	// and is not a positive boundary allow.
	iamCanPerformSkipBoundaryNotActionResource = "skipped_boundary_not_action_resource"
	// iamCanPerformSkipBoundaryUnresolved: the principal carries a boundary whose
	// document yielded no usable statements (missing/unresolved boundary document),
	// so the boundary ceiling cannot be proven permissive and the edge is dropped
	// conservatively.
	iamCanPerformSkipBoundaryUnresolved = "skipped_boundary_unresolved"
)

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
	skippedUncatalogued      int
	skippedAmbiguous         int
	skippedUnresolved        int
	skippedDeny              int
	skippedConditioned       int
	skippedNotActionResource int
	skippedSelfLoop          int
	// Permission-boundary intersection skips (PR4c). A candidate identity-policy
	// edge for a principal that has a boundary is counted here when the boundary
	// fails to make the action effective on the resolved resource.
	skippedBoundaryNoAllow           int
	skippedBoundaryDeny              int
	skippedBoundaryConditioned       int
	skippedBoundaryNotActionResource int
	skippedBoundaryUnresolved        int
}

// total returns the count of evaluations that produced no edge.
func (t iamCanPerformTally) total() int {
	return t.skippedUncatalogued + t.skippedAmbiguous + t.skippedUnresolved +
		t.skippedDeny + t.skippedConditioned + t.skippedNotActionResource +
		t.skippedSelfLoop + t.skippedBoundaryNoAllow + t.skippedBoundaryDeny +
		t.skippedBoundaryConditioned + t.skippedBoundaryNotActionResource +
		t.skippedBoundaryUnresolved
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
}

// iamCanPerformEdgeAccumulator collects, per (principal, resource) edge identity,
// the merged granted action set and the strongest resolution mode seen, so two
// catalog actions reaching the same resource converge on one idempotent edge.
type iamCanPerformEdgeAccumulator struct {
	actions map[string]struct{}
	sources map[string]struct{}
	mode    string
	// boundaryEvaluated is true when at least one identity-policy action on this
	// edge survived intersection with a principal permission boundary (PR4c). It is
	// false for unbounded principals and for resource-policy-only edges, so the
	// edge's boundary_evaluated property tells a query user whether the boundary
	// layer was considered for this edge.
	boundaryEvaluated bool
}

// ExtractIAMCanPerformEdges resolves each scanned IAM principal's trusted-Allow
// identity statements against the closed CAN_PERFORM catalog and emits a
// CAN_PERFORM edge for every (principal, resource) pair where a catalog action is
// granted (Allow, unconditioned, no NotAction/NotResource, not Deny-touched) AND
// the action's resource ARN resolves to EXACTLY ONE scanned CloudResource node of
// the catalog-expected type. It also evaluates resource-policy facts when the
// grantee is an exact scanned IAM role/user and the statement Resource applies to
// the attached resource. Wildcard / many / zero / public or unscanned principal /
// Deny / conditioned / NotAction / uncatalogued / self-loop all degrade to a
// counted skip, never an edge, and it never fabricates a node (graceful
// degradation).
//
// Returned edge rows are deduplicated by (principal_uid, resource_uid) with merged
// sorted actions and sorted deterministically so the batched write is stable
// across retries and reprojections (idempotent). The honesty boundary is encoded
// by the rel.grant_sources and rel.evaluation_scope properties the writer stamps.
func ExtractIAMCanPerformEdges(
	resourceEnvelopes []facts.Envelope,
	permissionEnvelopes []facts.Envelope,
	resourcePolicyEnvelopeSets ...[]facts.Envelope,
) IAMCanPerformResult {
	result := IAMCanPerformResult{EdgesByMode: make(map[string]int)}
	resourcePolicyEnvelopes := flattenResourcePolicyEnvelopeSets(resourcePolicyEnvelopeSets)
	if len(permissionEnvelopes) == 0 && len(resourcePolicyEnvelopes) == 0 {
		return result
	}

	index := buildCloudResourceJoinIndex(resourceEnvelopes)
	principals := groupIAMCanPerformByPrincipal(index, permissionEnvelopes, &result.Tally)
	catalog := iamCanPerformCatalogByAction()

	// edge identity -> merged granted action set + strongest resolution mode, so
	// several catalog actions to the same resource converge on one idempotent edge.
	edges := make(map[edgeKey]*iamCanPerformEdgeAccumulator)

	for _, principal := range principals {
		// Split a principal's statements into identity-policy statements (which can
		// positively grant) and permission-boundary statements (which only intersect /
		// ceiling, PR4c). A boundary statement never contributes to trustedActions, so
		// the identity grant is built from identity statements only.
		identityStmts, boundary := splitIAMCanPerformBoundary(principal.envelopes)
		grant := buildIAMCanPerformGrant(identityStmts, &result.Tally)

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
				// Permission-boundary intersection (PR4c): a principal WITH a boundary
				// needs both an identity allow AND a boundary allow on a covering
				// resource. An unbounded principal is unaffected (identity-only). A
				// conservative refusal is counted with its precise boundary reason.
				if !boundaryAllowsIAMCanPerform(boundary, entry, resourceUID, index, &result.Tally) {
					continue
				}
				addIAMCanPerformEdge(
					edges,
					principal.principalUID,
					resourceUID,
					entry.Action,
					mode,
					iamCanPerformGrantSourceIdentityPolicy,
				)
				if boundary.present {
					edges[edgeKey{principalUID: principal.principalUID, targetUID: resourceUID}].boundaryEvaluated = true
				}
			case iamTargetAmbiguous:
				result.Tally.skippedAmbiguous++
			default:
				result.Tally.skippedUnresolved++
			}
		}
	}

	addIAMCanPerformResourcePolicyEdges(index, resourcePolicyEnvelopes, catalog, edges, &result.Tally)
	result.Edges = buildIAMCanPerformEdgeRows(edges, result.EdgesByMode)
	return result
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
) []iamPrincipalStatements {
	byPrincipalARN := make(map[string][]facts.Envelope)
	order := make([]string, 0)
	for _, env := range permissionEnvelopes {
		if env.FactKind != facts.AWSIAMPermissionFactKind || env.IsTombstone {
			continue
		}
		principalARN := payloadString(env.Payload, "principal_arn")
		if principalARN == "" {
			continue
		}
		if _, seen := byPrincipalARN[principalARN]; !seen {
			order = append(order, principalARN)
		}
		byPrincipalARN[principalARN] = append(byPrincipalARN[principalARN], env)
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
		principals = append(principals, iamPrincipalStatements{principalUID: uid, envelopes: byPrincipalARN[principalARN]})
	}
	sort.Slice(principals, func(a, b int) bool {
		return principals[a].principalUID < principals[b].principalUID
	})
	return principals
}

func addIAMCanPerformEdge(
	edges map[edgeKey]*iamCanPerformEdgeAccumulator,
	principalUID string,
	resourceUID string,
	action string,
	mode string,
	source string,
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
			"principal_uid":      key.principalUID,
			"resource_uid":       key.targetUID,
			"actions":            actions,
			"action_count":       len(actions),
			"evaluation_scope":   iamCanPerformEvaluationScopeForSources(grantSources, acc.boundaryEvaluated),
			"grant_sources":      grantSources,
			"boundary_evaluated": acc.boundaryEvaluated,
		})
	}
	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["principal_uid"]) + "->" + anyToString(rows[a]["resource_uid"])
		right := anyToString(rows[b]["principal_uid"]) + "->" + anyToString(rows[b]["resource_uid"])
		return left < right
	})
	return rows
}

// iamCanPerformEvaluationScopeForSources derives the honesty label from the grant
// sources that armed the edge and whether the principal's permission boundary was
// intersected (PR4c). When a resource policy is one of the sources, the
// resource-policy scope labels take precedence and boundary intersection is not
// expressed (a boundary ceilings identity grants, not resource-policy grants). For
// an identity-only edge, a boundary that was evaluated promotes the label to
// identity_policy_and_boundary so a query user can tell the boundary layer was
// considered.
func iamCanPerformEvaluationScopeForSources(sources []string, boundaryEvaluated bool) string {
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
	case hasIdentity && hasResource:
		return iamCanPerformEvaluationScopeIdentityAndResourcePolicy
	case hasResource:
		return iamCanPerformEvaluationScopeResourcePolicyOnly
	case boundaryEvaluated:
		return iamCanPerformEvaluationScopeIdentityPolicyBoundary
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
