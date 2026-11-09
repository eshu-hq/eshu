// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"
	"time"
)

// Suppression carry sub-signals (#10408 L3). They ride the pass result's
// sub-signals the way the advisory-group counts do, so an operator without a
// metrics backend still sees what a re-key wave carried or orphaned. L7
// promotes them to instruments; no new instrument ships here.
const (
	// suppressionCarriedViaMemberSubSignal counts new findings a suppression
	// still hides through a member anchor that is not the finding's
	// canonical key: the coverage the re-key carried across.
	suppressionCarriedViaMemberSubSignal = "suppression_carried_via_member"
	// suppressionOrphanedBySplitSubSignal counts loaded, unexpired
	// suppressions naming a re-key-touched id that no new finding cites
	// anymore (a scope-mismatch citation still cites): the coverage a split
	// dropped, so the operator can re-issue deliberately.
	suppressionOrphanedBySplitSubSignal = "suppression_orphaned_by_split"
)

// suppressionCarryTally is one pass's carried/orphaned suppression coverage.
type suppressionCarryTally struct {
	carried  int
	orphaned int
}

// suppressionHidesFinding reports whether a suppression decision keeps the
// finding hidden. Expired, scope-mismatched, and advisory-retracted findings
// stay visible (with their provenance preserved for audit); only the
// suppressed family and provider dismissals hide.
func suppressionHidesFinding(state SupplyChainSuppressionState) bool {
	switch state {
	case SupplyChainSuppressionStateNotAffected,
		SupplyChainSuppressionStateAcceptedRisk,
		SupplyChainSuppressionStateFalsePositive,
		SupplyChainSuppressionStateIgnored,
		SupplyChainSuppressionStateProviderDismissed:
		return true
	default:
		return false
	}
}

// citesSuppressionLive reports whether any new finding still reaches the
// suppression: a hiding decision, or a scope-mismatch adjacency. An expired,
// active, or retraction citation means the suppression no longer covers the
// finding.
func citesSuppressionLive(findings []SupplyChainImpactFinding, suppressionID string) bool {
	if suppressionID == "" {
		return false
	}
	for _, finding := range findings {
		if finding.Suppression.SuppressionID != suppressionID {
			continue
		}
		if suppressionHidesFinding(finding.Suppression.State) ||
			finding.Suppression.State == SupplyChainSuppressionStateScopeMismatch {
			return true
		}
	}
	return false
}

// memberAnchorCarry reports whether the suppression hid the finding through a
// member anchor that is not the finding's group key (#10408 D4/D6: the scope
// CVEID anchor matches the group id or any CVE member, the advisory anchor
// the deciding record's id or any advisory member). An anchor equal to the
// group key may have matched canonically, so only a strictly non-key member
// anchor counts as carried coverage.
func memberAnchorCarry(finding SupplyChainImpactFinding, scope vulnerabilitySuppressionScope) bool {
	members := finding.AdvisoryGroupMembers
	anchor := func(named string) bool {
		if strings.TrimSpace(named) == "" || strings.EqualFold(named, finding.CVEID) {
			return false
		}
		for _, member := range members {
			if strings.EqualFold(named, strings.TrimSpace(member)) {
				return true
			}
		}
		return false
	}
	return anchor(scope.CVEID) || anchor(scope.AdvisoryID)
}

// countSuppressionCarry tallies one pass's carried and orphaned suppression
// coverage over the loaded suppressions, the evaluated new findings, the
// writer's touched group set, and the member-expanded changed set. Carried
// counts every hiding decision reached through a non-canonical member
// anchor, on any pass. Orphaned counts only on a pass that changed group
// identity (a non-empty touched or changed set): a loaded, unexpired
// suppression naming an in-play id that no new finding cites anymore. The
// in-play set is the touched set plus the member-expanded changed set
// (#10408 F1): a suppression naming a non-group member id that the re-key
// unioned into ChangedCVEIDs was in play even though no touched group names
// it, so dropping it still orphans coverage. The gate keeps ordinary
// coverage loss off the split signal: on a no-change pass an uncited
// suppression is not a split orphan.
func countSuppressionCarry(
	suppressions []vulnerabilitySuppression,
	findings []SupplyChainImpactFinding,
	touchedGroupIDs []string,
	changedIDs []string,
	now time.Time,
) suppressionCarryTally {
	var tally suppressionCarryTally
	byID := make(map[string]vulnerabilitySuppression, len(suppressions))
	for _, suppression := range suppressions {
		if suppression.SuppressionID == "" {
			continue
		}
		byID[suppression.SuppressionID] = suppression
	}
	for _, finding := range findings {
		decision := finding.Suppression
		if decision.SuppressionID == "" || !suppressionHidesFinding(decision.State) {
			continue
		}
		suppression, ok := byID[decision.SuppressionID]
		if !ok {
			continue
		}
		if memberAnchorCarry(finding, suppression.Scope) {
			tally.carried++
		}
	}
	if len(touchedGroupIDs) == 0 && len(changedIDs) == 0 {
		return tally
	}
	newIDs := make(map[string]struct{})
	addFolded := func(values ...string) {
		for _, value := range values {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				newIDs[strings.ToLower(trimmed)] = struct{}{}
			}
		}
	}
	for _, finding := range findings {
		addFolded(finding.CVEID, finding.AdvisoryID)
		addFolded(finding.AdvisoryGroupMembers...)
	}
	touched := make(map[string]struct{}, len(touchedGroupIDs)+len(changedIDs))
	for _, id := range touchedGroupIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			touched[strings.ToLower(trimmed)] = struct{}{}
		}
	}
	for _, id := range changedIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			touched[strings.ToLower(trimmed)] = struct{}{}
		}
	}
	for _, suppression := range suppressions {
		if suppression.SuppressionID == "" || suppressionIsExpired(suppression, now) {
			continue
		}
		if citesSuppressionLive(findings, suppression.SuppressionID) {
			continue
		}
		orphan := false
		for _, named := range []string{suppression.Scope.CVEID, suppression.Scope.AdvisoryID} {
			folded := strings.ToLower(strings.TrimSpace(named))
			if folded == "" {
				continue
			}
			if _, ok := touched[folded]; !ok {
				continue
			}
			if _, ok := newIDs[folded]; !ok {
				orphan = true
			}
		}
		if orphan {
			tally.orphaned++
		}
	}
	return tally
}
