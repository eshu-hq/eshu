package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// iamCanPerformBoundary is one principal's permission-boundary ceiling, derived
// from its aws_iam_permission facts whose policy_source is "boundary" (PR4c). A
// boundary never positively grants an action; it intersects (ceilings) the
// identity-policy grant set: a bounded principal's identity allow becomes effective
// only if the boundary ALSO allows that action on a covering resource. The grant
// reuses iamPrincipalGrant for the trusted-Allow union, the Deny union, and the
// per-action statements (so resource coverage can be checked), all built from
// boundary statements only.
type iamCanPerformBoundary struct {
	// present is true when the principal carries at least one boundary statement,
	// i.e. it has a permission boundary attached. An unbounded principal (present
	// == false) is evaluated identity-only and is never intersected.
	present bool
	// hadUsableStatement is true when at least one boundary statement was parseable
	// into an Allow or Deny. A boundary that was referenced but yielded no usable
	// statement (missing/unresolved document) is conservatively non-permissive.
	hadUsableStatement bool
	grant              iamPrincipalGrant
	// conditionedActions / notActionResourceActions record catalog actions whose
	// ONLY boundary coverage was a conditioned or NotAction/NotResource Allow
	// statement, so the precise conservative skip reason is visible when such an
	// action is the candidate. A trusted (clean) Allow for the same action wins and
	// removes the action from these sets.
	conditionedActions       map[string]struct{}
	notActionResourceActions map[string]struct{}
}

// splitIAMCanPerformBoundary partitions a principal's permission facts into its
// identity-policy statements (which can positively grant) and its
// permission-boundary ceiling (which only intersects). Boundary statements are the
// aws_iam_permission facts whose policy_source is "boundary"; everything else is an
// identity statement. The boundary grant is built from boundary statements only, so
// a boundary statement never contributes to the identity grant's trustedActions.
//
// Conditioned or NotAction/NotResource boundary Allow statements carrying a catalog
// action are recorded as the matching conservative reason (not as a permissive
// boundary allow), mirroring the identity grant builder's precedence. The tally is
// not touched here; boundaryAllowsIAMCanPerform counts the precise reason per
// candidate edge so a multi-action principal counts one reason per suppressed edge.
func splitIAMCanPerformBoundary(
	envelopes []facts.Envelope,
) ([]facts.Envelope, iamCanPerformBoundary) {
	identity := make([]facts.Envelope, 0, len(envelopes))
	boundary := iamCanPerformBoundary{
		grant: iamPrincipalGrant{
			trustedActions:     make(map[string]struct{}),
			denyActions:        make(map[string]struct{}),
			statementsByAction: make(map[string][]facts.Envelope),
		},
		conditionedActions:       make(map[string]struct{}),
		notActionResourceActions: make(map[string]struct{}),
	}
	catalogActions := iamCanPerformCatalogActions()

	for _, env := range envelopes {
		if env.IsTombstone {
			continue
		}
		if payloadString(env.Payload, "policy_source") != iamCanPerformPolicySourceBoundary {
			identity = append(identity, env)
			continue
		}
		// A non-tombstone boundary fact means the principal has a permission boundary.
		boundary.present = true
		foldIAMCanPerformBoundaryStatement(env, &boundary, catalogActions)
	}
	return identity, boundary
}

// foldIAMCanPerformBoundaryStatement folds one boundary statement into the boundary
// ceiling: Deny actions join denyActions; clean Allow statements (unconditioned, no
// NotAction/NotResource) join trustedActions and record their statement for resource
// coverage; a conditioned or NotAction/NotResource Allow carrying a catalog action
// records the conservative reason instead.
func foldIAMCanPerformBoundaryStatement(
	env facts.Envelope,
	boundary *iamCanPerformBoundary,
	catalogActions map[string]struct{},
) {
	effect := payloadString(env.Payload, "effect")
	actions := payloadStringSlice(env.Payload, "actions")
	hasConditions := payloadBool(env.Payload, "has_conditions")
	hasNotActions := len(payloadStringSlice(env.Payload, "not_actions")) > 0
	hasNotResources := len(payloadStringSlice(env.Payload, "not_resources")) > 0

	if effect == "Deny" {
		boundary.hadUsableStatement = true
		for _, action := range actions {
			boundary.grant.denyActions[action] = struct{}{}
		}
		return
	}
	if effect != "Allow" {
		return
	}
	boundary.hadUsableStatement = true

	if hasConditions || hasNotActions || hasNotResources {
		// Not a conservatively-permissive boundary allow. Record the precise reason
		// for any catalog action it carries so a suppressed candidate edge is
		// explained. Conditions win the label when both are present, matching the
		// identity grant builder's precedence.
		for _, action := range actions {
			if !iamCanPerformActionIsCatalogued(action, catalogActions) {
				continue
			}
			if hasConditions {
				boundary.conditionedActions[action] = struct{}{}
			} else {
				boundary.notActionResourceActions[action] = struct{}{}
			}
		}
		return
	}

	for _, action := range actions {
		boundary.grant.trustedActions[action] = struct{}{}
		boundary.grant.statementsByAction[action] = append(boundary.grant.statementsByAction[action], env)
		// A clean Allow supersedes any conservative reason recorded for the action.
		delete(boundary.conditionedActions, action)
		delete(boundary.notActionResourceActions, action)
	}
}

// boundaryAllowsIAMCanPerform decides whether a candidate identity-policy edge for
// one catalog action on one resolved resource survives intersection with the
// principal's permission boundary, counting the precise conservative reason on a
// suppression. An unbounded principal (boundary.present == false) is unaffected and
// always passes. A bounded principal passes only when the boundary explicitly does
// NOT deny the action AND the boundary has a clean Allow covering the action whose
// resource patterns cover the resolved resource.
func boundaryAllowsIAMCanPerform(
	boundary iamCanPerformBoundary,
	entry iamCanPerformAction,
	resourceUID string,
	index cloudResourceJoinIndex,
	tally *iamCanPerformTally,
) bool {
	if !boundary.present {
		return true
	}
	if boundary.grant.denied(entry.Action) {
		tally.skippedBoundaryDeny++
		return false
	}
	if !boundary.hadUsableStatement {
		// A boundary was attached but its document yielded no usable statement
		// (missing / unresolved). The ceiling cannot be proven permissive.
		tally.skippedBoundaryUnresolved++
		return false
	}
	if boundary.grant.allows(entry.Action) &&
		iamCanPerformBoundaryCoversResource(boundary.grant, entry, resourceUID, index) {
		return true
	}
	// The boundary does not cleanly allow this action on the resolved resource.
	// Prefer the precise conservative reason when the only coverage was a
	// conditioned or NotAction/NotResource Allow.
	switch {
	case isCanPerformBoundaryActionIn(boundary.conditionedActions, entry.Action):
		tally.skippedBoundaryConditioned++
	case isCanPerformBoundaryActionIn(boundary.notActionResourceActions, entry.Action):
		tally.skippedBoundaryNotActionResource++
	default:
		tally.skippedBoundaryNoAllow++
	}
	return false
}

// iamCanPerformBoundaryCoversResource reports whether any boundary Allow statement
// that grants the action carries a resource pattern covering the resolved resource.
// Unlike positive-grant target resolution, a boundary is a CEILING, so a wildcard or
// prefix resource pattern legitimately covers the specific resolved resource (it
// does not need to name exactly one node). Coverage matches an exact ARN, a "/"
// prefix, a glob, or "*".
func iamCanPerformBoundaryCoversResource(
	grant iamPrincipalGrant,
	entry iamCanPerformAction,
	resourceUID string,
	index cloudResourceJoinIndex,
) bool {
	resourceARN := iamCanPerformARNForUID(index, resourceUID)
	if resourceARN == "" {
		return false
	}
	for _, pattern := range collectTrustedResources(grant.statementsCovering(entry.Action)) {
		switch {
		case pattern == "*":
			return true
		case pattern == resourceARN:
			return true
		case strings.HasPrefix(pattern, resourceARN+"/"):
			return true
		case strings.ContainsAny(pattern, "*?") && globMatch(pattern, resourceARN):
			return true
		}
	}
	return false
}

// iamCanPerformARNForUID reverse-resolves a resolved CloudResource uid back to the
// scanned ARN it was keyed on. Catalog resources are keyed on their ARN, so the
// inverse of the join index's byARN map is unambiguous for these nodes.
func iamCanPerformARNForUID(index cloudResourceJoinIndex, uid string) string {
	for arn, candidate := range index.byARN {
		if candidate == uid {
			return arn
		}
	}
	return ""
}

// isCanPerformBoundaryActionIn reports whether the action (or a covering "*" /
// "service:*" wildcard the boundary statement used) is present in the conservative
// reason set, so the precise skip reason survives a wildcard-shaped boundary
// statement.
func isCanPerformBoundaryActionIn(set map[string]struct{}, action string) bool {
	if _, ok := set[action]; ok {
		return true
	}
	if _, ok := set["*"]; ok {
		return true
	}
	if service, _, ok := strings.Cut(action, ":"); ok {
		if _, ok := set[service+":*"]; ok {
			return true
		}
	}
	return false
}
