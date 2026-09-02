// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcan

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/iampolicy"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// buildIAMCanPerformGrant folds a principal's identity-policy statements into its
// trusted grant for CAN_PERFORM resolution. It reuses the shared iampolicy.PrincipalGrant
// shape (and its allows/denied/statementsCovering helpers) but accounts skips into
// the CAN_PERFORM tally and against the CAN_PERFORM catalog: a statement
// contributes its actions to trustedActions only when it is Allow, unconditioned,
// and free of NotAction/NotResource. Deny statements contribute to denyActions.
// A conditioned or NotAction/NotResource Allow statement that carries a
// catalog-relevant action is counted as the matching skip reason so an operator
// sees why an action that "looks" granted did not arm; it does NOT contribute to
// trustedActions. Uncatalogued trusted actions are counted and do not arm the
// grant. This is the honest under-approximation: conditions carry key names only,
// never values, and out-of-vocabulary actions have no closed target semantics.
//
// It is a distinct function from buildIAMPrincipalGrant (the escalation builder)
// because the two slices count into different tally types and against different
// catalogs; the grant struct and its lookup helpers are shared.
func buildIAMCanPerformGrant(statements []iampolicy.Statement, tally *iamCanPerformTally) iampolicy.PrincipalGrant {
	grant := iampolicy.PrincipalGrant{
		TrustedActions:     make(map[string]struct{}),
		DenyActions:        make(map[string]struct{}),
		StatementsByAction: make(map[string][]iampolicy.Statement),
	}
	catalogActions := iamCanPerformCatalogActions()

	for _, statement := range statements {
		if !iamCanPerformIdentityPolicySource(statement.Permission.PolicySource) {
			continue
		}
		actions := statement.Permission.Actions
		hasConditions := payloadcore.DerefBool(statement.Permission.HasConditions)
		hasNotActions := len(statement.Permission.NotActions) > 0
		hasNotResources := len(statement.Permission.NotResources) > 0

		if statement.Permission.Effect == "Deny" {
			for _, action := range actions {
				grant.DenyActions[action] = struct{}{}
			}
			continue
		}
		if statement.Permission.Effect != "Allow" {
			continue
		}

		if hasConditions || hasNotActions || hasNotResources {
			// Cannot be conservatively trusted. If it carries a catalog action, count
			// the precise reason so the skip is visible rather than silent. Conditions
			// win the label when both are present (a conditioned NotAction statement is
			// reported skipped_conditioned) to match the escalation slice's precedence.
			if iampolicy.StatementTouchesCatalog(actions, catalogActions) {
				if hasConditions {
					tally.recordSkip(iamCanPerformSkipConditioned)
				} else {
					tally.recordSkip(iamCanPerformSkipNotActionResource)
				}
			}
			continue
		}

		for _, action := range actions {
			if !iamCanPerformActionIsCatalogued(action, catalogActions) {
				tally.skippedUncatalogued++
				continue
			}
			grant.TrustedActions[action] = struct{}{}
			grant.StatementsByAction[action] = append(grant.StatementsByAction[action], statement)
		}
	}
	return grant
}

func iamCanPerformIdentityPolicySource(policySource string) bool {
	switch policySource {
	case iamCanPerformPolicySourceInline, iamCanPerformPolicySourceAttachedManaged:
		return true
	default:
		return false
	}
}
