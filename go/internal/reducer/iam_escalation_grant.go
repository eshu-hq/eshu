// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
)

// iamPrincipalGrant is one principal's conservatively-trusted effective grant: the
// union of actions from its trusted Allow statements (Allow, unconditioned, no
// NotAction/NotResource), the union of actions touched by any Deny statement, and
// the trusted statements themselves so target resolution can read the right
// resources. Conditioned / NotAction-bearing statements are counted as skips when
// they are the only carrier of a catalog action (see buildIAMPrincipalGrant).
type iamPrincipalGrant struct {
	trustedActions map[string]struct{}
	denyActions    map[string]struct{}
	// statementsByAction maps a lowercase action to the decoded trusted
	// statements that granted it, so the target resolver can pull the resources
	// of the exact statement carrying a primitive's action (e.g. the iam:passrole
	// statement). Each statement carries its source FactID so statementsCovering
	// can deduplicate a statement registered under several lookup keys, exactly as
	// the pre-typing envelope-based path did.
	statementsByAction map[string][]iamPermissionStatement
}

// iamPermissionStatement pairs a decoded aws_iam_permission statement with its
// source FactID so the grant's statementsByAction map can deduplicate a
// statement that matches more than one action-lookup key.
type iamPermissionStatement struct {
	factID     string
	permission iamv1.Permission
}

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

// armStatus reports whether the principal holds every action a primitive requires
// (honoring * and service:* wildcards) and whether a Deny blocks it. A Deny on any
// required action wins (iamPrimitiveDenied) so the conservative under-approximation
// removes the principal from the primitive. It is the AND gate for multi-action
// primitives.
func (g iamPrincipalGrant) armStatus(primitive iamEscalationPrimitive) iamPrimitiveArmStatus {
	for _, action := range primitive.Actions {
		if g.denied(action) {
			return iamPrimitiveDenied
		}
	}
	for _, action := range primitive.Actions {
		if !g.allows(action) {
			return iamPrimitiveIncomplete
		}
	}
	return iamPrimitiveArmed
}

// allows reports whether the trusted action set covers an action, honoring the
// two unambiguous wildcard shapes: "*" (all actions) and "service:*" (the
// action's service). Partial wildcards like "iam:Create*" are intentionally not
// expanded (conservative).
func (g iamPrincipalGrant) allows(action string) bool {
	if _, ok := g.trustedActions[action]; ok {
		return true
	}
	if _, ok := g.trustedActions["*"]; ok {
		return true
	}
	if service, _, ok := strings.Cut(action, ":"); ok {
		if _, ok := g.trustedActions[service+":*"]; ok {
			return true
		}
	}
	return false
}

// statementsCovering returns the trusted statements whose action set grants the
// given carrier action, either exactly or via a "*"/"service:*" wildcard. A
// wildcard grant ("iam:*") registers its statement under the wildcard token, so a
// concrete-action lookup would miss it; this method resolves the carrier action to
// every statement that actually covers it, so the target resolver reads the right
// resources for a wildcard-granted primitive. Duplicate statements are returned at
// most once.
func (g iamPrincipalGrant) statementsCovering(action string) []iamPermissionStatement {
	keys := []string{action, "*"}
	if service, _, ok := strings.Cut(action, ":"); ok {
		keys = append(keys, service+":*")
	}
	seen := make(map[string]struct{})
	out := make([]iamPermissionStatement, 0)
	for _, key := range keys {
		for _, statement := range g.statementsByAction[key] {
			if _, dup := seen[statement.factID]; dup && statement.factID != "" {
				continue
			}
			if statement.factID != "" {
				seen[statement.factID] = struct{}{}
			}
			out = append(out, statement)
		}
	}
	return out
}

// denied reports whether a Deny statement touches the action exactly or via a
// "*"/"service:*" wildcard. A Deny on a primitive's action removes the principal
// from that primitive entirely (conservative under-approximation).
func (g iamPrincipalGrant) denied(action string) bool {
	if _, ok := g.denyActions[action]; ok {
		return true
	}
	if _, ok := g.denyActions["*"]; ok {
		return true
	}
	if service, _, ok := strings.Cut(action, ":"); ok {
		if _, ok := g.denyActions[service+":*"]; ok {
			return true
		}
	}
	return false
}

// buildIAMPrincipalGrant folds a principal's statements into its trusted grant.
// A statement contributes its actions to trustedActions only when it is Allow,
// unconditioned, and free of NotAction/NotResource. Deny statements contribute to
// denyActions. Conditioned or NotAction/NotResource Allow statements that carry a
// catalog-relevant action are counted as the matching skip reason so an operator
// sees why a primitive that "looks" granted did not arm; sts:AssumeRole anywhere
// is counted as deferred.
func buildIAMPrincipalGrant(statements []iamPermissionStatement, tally *iamEscalationTally) iamPrincipalGrant {
	grant := iamPrincipalGrant{
		trustedActions:     make(map[string]struct{}),
		denyActions:        make(map[string]struct{}),
		statementsByAction: make(map[string][]iamPermissionStatement),
	}
	catalogActions := iamEscalationCatalogActions()
	deferredCounted := false

	for _, statement := range statements {
		actions := statement.permission.Actions
		hasConditions := boolPtrValue(statement.permission.HasConditions)
		hasNotActions := len(statement.permission.NotActions) > 0
		hasNotResources := len(statement.permission.NotResources) > 0

		if statement.permission.Effect == "Deny" {
			for _, action := range actions {
				grant.denyActions[action] = struct{}{}
			}
			continue
		}
		if statement.permission.Effect != "Allow" {
			continue
		}

		// sts:AssumeRole is recognized and deferred to the CAN_ASSUME edge. Count it
		// once per principal regardless of how many statements carry it.
		if !deferredCounted && allowStatementTouches(actions, iamEscalationStsAssumeRoleAction) {
			tally.deferredCanAssume++
			deferredCounted = true
		}

		if hasConditions || hasNotActions || hasNotResources {
			// Cannot be conservatively trusted. If it carries a catalog action, count
			// the precise reason so the skip is visible rather than silent. It does not
			// contribute to trustedActions.
			if statementTouchesCatalog(actions, catalogActions) {
				if hasConditions {
					tally.skippedConditioned++
				} else {
					tally.skippedNotActionResource++
				}
			}
			continue
		}

		for _, action := range actions {
			grant.trustedActions[action] = struct{}{}
			grant.statementsByAction[action] = append(grant.statementsByAction[action], statement)
		}
	}
	return grant
}

// allowStatementTouches reports whether an action set contains the target action
// exactly or via a "*"/"service:*" wildcard.
func allowStatementTouches(actions []string, target string) bool {
	for _, action := range actions {
		if action == target || action == "*" {
			return true
		}
		if service, _, ok := strings.Cut(target, ":"); ok && action == service+":*" {
			return true
		}
	}
	return false
}

// statementTouchesCatalog reports whether any of a statement's actions is a
// catalog action (exact, "*", or "service:*"). Used only to decide whether an
// untrusted (conditioned / NotAction) statement is worth counting as a skip.
func statementTouchesCatalog(actions []string, catalogActions map[string]struct{}) bool {
	for _, action := range actions {
		if action == "*" {
			return true
		}
		if _, ok := catalogActions[action]; ok {
			return true
		}
		if service, _, ok := strings.Cut(action, ":"); ok && service != "" {
			wildcard := service + ":*"
			for catalogAction := range catalogActions {
				if catalogAction == wildcard {
					return true
				}
				if cs, _, ok := strings.Cut(catalogAction, ":"); ok && cs == service {
					return true
				}
			}
		}
	}
	return false
}
