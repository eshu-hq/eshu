// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/reducer/iampolicy"

// iamPrimitiveArmStatus is the outcome of evaluating whether a principal holds a
// primitive: armed, blocked by a Deny, or simply not granted.
type iamPrimitiveArmStatus int

const (
	// iamPrimitiveArmed means every required action is present and unblocked.
	iamPrimitiveArmed iamPrimitiveArmStatus = iota
	// iamPrimitiveDenied means a Deny touches one of the required actions, so the
	// primitive is conservatively removed regardless of the Allow grants.
	iamPrimitiveDenied
	// iamPrimitiveIncomplete means at least one required action is not granted.
	iamPrimitiveIncomplete
)

// grantArmStatus reports whether the principal holds every action a primitive
// requires (honoring * and service:* wildcards) and whether a Deny blocks it. A
// Deny on any required action wins (iamPrimitiveDenied) so the conservative
// under-approximation removes the principal from the primitive. It is the AND
// gate for multi-action primitives.
//
// It is a free function, not a method: the grant shape now lives in [iampolicy]
// so the reducer root cannot attach methods to it, and the escalation primitive
// vocabulary it reads is root-owned.
func grantArmStatus(g iamPrincipalGrant, primitive iamEscalationPrimitive) iamPrimitiveArmStatus {
	for _, action := range primitive.Actions {
		if g.Denied(action) {
			return iamPrimitiveDenied
		}
	}
	for _, action := range primitive.Actions {
		if !g.Allows(action) {
			return iamPrimitiveIncomplete
		}
	}
	return iamPrimitiveArmed
}

// buildIAMPrincipalGrant folds a principal's statements into its trusted grant.
// A statement contributes its actions to TrustedActions only when it is Allow,
// unconditioned, and free of NotAction/NotResource. Deny statements contribute to
// DenyActions. Conditioned or NotAction/NotResource Allow statements that carry a
// catalog-relevant action are counted as the matching skip reason so an operator
// sees why a primitive that "looks" granted did not arm; sts:AssumeRole anywhere
// is counted as deferred.
func buildIAMPrincipalGrant(statements []iamPermissionStatement, tally *iamEscalationTally) iamPrincipalGrant {
	grant := iamPrincipalGrant{
		TrustedActions:     make(map[string]struct{}),
		DenyActions:        make(map[string]struct{}),
		StatementsByAction: make(map[string][]iamPermissionStatement),
	}
	catalogActions := iamEscalationCatalogActions()
	deferredCounted := false

	for _, statement := range statements {
		shape := iampolicy.Classify(statement)
		actions := shape.Actions

		if shape.Effect == iampolicy.EffectDeny {
			for _, action := range actions {
				grant.DenyActions[action] = struct{}{}
			}
			continue
		}
		if shape.Effect != iampolicy.EffectAllow {
			continue
		}

		// sts:AssumeRole is recognized and deferred to the CAN_ASSUME edge. Count it
		// once per principal regardless of how many statements carry it.
		if !deferredCounted && allowStatementTouches(actions, iamEscalationStsAssumeRoleAction) {
			tally.deferredCanAssume++
			deferredCounted = true
		}

		if !shape.Trustable() {
			// Cannot be conservatively trusted. If it carries a catalog action, count
			// the precise reason so the skip is visible rather than silent. It does not
			// contribute to trustedActions.
			if statementTouchesCatalog(actions, catalogActions) {
				if shape.HasConditions {
					tally.skippedConditioned++
				} else {
					tally.skippedNotActionResource++
				}
			}
			continue
		}

		for _, action := range actions {
			grant.TrustedActions[action] = struct{}{}
			grant.StatementsByAction[action] = append(grant.StatementsByAction[action], statement)
		}
	}
	return grant
}
