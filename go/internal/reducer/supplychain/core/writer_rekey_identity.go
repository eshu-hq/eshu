// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import "strings"

// RekeyReport is one pass's genuine group-identity change (#10408 D6): the
// closed outcome, the identity-changed groups, and the magnitudes, all
// limited to rows whose coalesced group id or member set differs from the
// replaced stored row plus group retirements with no successor. An ordinary
// pass (same groups, same members: an EPSS-only refresh, a new admission, a
// generation rollover, a same-key rewrite) reports Count false and stays
// silent; the counter must never fire on commit shape alone.
type RekeyReport struct {
	Outcome    RekeyOutcome
	Groups     []string
	Findings   int
	Tombstoned int
	Count      bool
}

// reportRekeyIdentity derives the pass's genuine group-identity change from
// the rows it committed and the stored rows it replaced or retired (#10408).
// added holds new-row groups no stored row names, removed holds stored-row
// groups no new row names; a removed group whose stored members survive in
// the new rows migrated (merge), an added group whose members were all
// already stored split off (split). Member-only refreshes keep the same
// groups with different members; retire-only drops groups whose members
// appear in no new row. Bootstrap and pure admission name only new groups
// with brand-new members and stay silent.
func reportRekeyIdentity(rows []changedRow, stored []storedFinding, generationID string, fencingToken int64, retract bool) RekeyReport {
	own := map[string]storedFinding{}
	prior := map[string]storedFinding{}
	for _, row := range stored {
		if row.generationID == generationID {
			own[row.factID] = row
		} else {
			prior[row.stableKey] = row
		}
	}
	rerun := len(own) > 0
	newGroups := map[string]struct{}{}
	oldGroups := map[string]struct{}{}
	newMembers := map[string]struct{}{}
	storedMembers := map[string]struct{}{}
	newGroupMembers := map[string]map[string]struct{}{}
	storedGroupMembers := map[string]map[string]struct{}{}
	groupOf := func(cveID, advisoryID string) string {
		return coalescedFindingGroupID(cveID, advisoryID)
	}
	for _, row := range rows {
		g := groupOf(row.cveID, row.advisoryID)
		if g != "" {
			newGroups[g] = struct{}{}
		}
		for _, m := range row.members {
			if m = strings.TrimSpace(m); m != "" {
				newMembers[m] = struct{}{}
				if g != "" {
					if newGroupMembers[g] == nil {
						newGroupMembers[g] = map[string]struct{}{}
					}
					newGroupMembers[g][m] = struct{}{}
				}
			}
		}
	}
	for _, row := range stored {
		g := groupOf(row.cveID, row.advisoryID)
		if g != "" {
			oldGroups[g] = struct{}{}
		}
		for _, m := range row.members {
			if m = strings.TrimSpace(m); m != "" {
				storedMembers[m] = struct{}{}
				if g != "" {
					if storedGroupMembers[g] == nil {
						storedGroupMembers[g] = map[string]struct{}{}
					}
					storedGroupMembers[g][m] = struct{}{}
				}
			}
		}
	}
	// Identity-changed rows: replaced under a different group id or member
	// set. No-base rows are admissions, never identity changes on their own.
	changedGroups := map[string]struct{}{}
	findings := 0
	memberDiff := false
	for _, row := range rows {
		var (
			base storedFinding
			ok   bool
		)
		if rerun {
			base, ok = own[row.factID]
		} else {
			base, ok = prior[row.stableKey]
		}
		if !ok {
			continue
		}
		if groupOf(row.cveID, row.advisoryID) == groupOf(base.cveID, base.advisoryID) &&
			memberSetsEqual(row.members, base.members) {
			continue
		}
		if g := groupOf(row.cveID, row.advisoryID); g != "" {
			changedGroups[g] = struct{}{}
		}
		if !memberSetsEqual(row.members, base.members) {
			memberDiff = true
		}
		findings++
	}
	// Genuine retirements: unkept stored rows in groups no new row and no
	// kept stored row names. Mirrors the retraction keep rules, and only on
	// a retracting pass; a partial pass retracts nothing.
	retiredGroups := map[string]struct{}{}
	tombstoned := 0
	if retract {
		keptIDs := make(map[string]struct{}, len(rows))
		keptKeys := make(map[string]struct{}, len(rows))
		for _, row := range rows {
			keptIDs[row.factID] = struct{}{}
			keptKeys[row.stableKey] = struct{}{}
		}
		unkept := map[string]storedFinding{}
		for _, row := range stored {
			if rerun {
				if _, kept := keptIDs[row.factID]; kept || row.fencingToken > fencingToken {
					continue
				}
			} else {
				if _, kept := keptKeys[row.stableKey]; kept {
					continue
				}
			}
			unkept[row.factID+row.stableKey] = row
		}
		keptGroups := map[string]struct{}{}
		for _, row := range stored {
			var kept bool
			if rerun {
				_, kept = keptIDs[row.factID]
			} else {
				_, kept = keptKeys[row.stableKey]
			}
			if kept {
				if g := groupOf(row.cveID, row.advisoryID); g != "" {
					keptGroups[g] = struct{}{}
				}
			}
		}
		for _, row := range unkept {
			g := groupOf(row.cveID, row.advisoryID)
			if g == "" {
				continue
			}
			if _, ok := newGroups[g]; ok {
				continue
			}
			if _, ok := keptGroups[g]; ok {
				continue
			}
			retiredGroups[g] = struct{}{}
			tombstoned++
		}
	}
	return classifyRekeyGroups(newGroups, oldGroups, newMembers, storedMembers,
		newGroupMembers, storedGroupMembers, changedGroups, retiredGroups,
		memberDiff, findings, tombstoned, rows, own, prior, rerun, retract)
}

// classifyRekeyGroups maps the identity-change sets to the closed D6 outcome.
// added holds new-row groups no stored row names, removed holds stored-row
// groups no new row names. A removed group whose stored members survive in
// the new rows migrated (merge); an added group whose members were all
// already stored split off (split). Both nonempty prefers split (a fragment
// landing on a fresh key needs deliberate re-issue); neither with member
// drift is member-only; silence covers bootstrap, admission, rollover, and
// same-key rewrites.
func classifyRekeyGroups(newGroups, oldGroups, newMembers, storedMembers map[string]struct{},
	newGroupMembers, storedGroupMembers map[string]map[string]struct{},
	changedGroups, retiredGroups map[string]struct{}, memberDiff bool,
	findings, tombstoned int, rows []changedRow,
	own, prior map[string]storedFinding, rerun, retract bool,
) RekeyReport {
	silent := RekeyReport{}
	added := differenceOfSets(newGroups, oldGroups)
	removed := differenceOfSets(oldGroups, newGroups)
	// A partial-evidence pass retracts nothing (upsert-only fallback keys),
	// so it can retire no group even when it fails to derive one.
	if !retract {
		removed = map[string]struct{}{}
	}
	subsetOf := func(needles map[string]struct{}, haystack map[string]struct{}) bool {
		for needle := range needles {
			if _, ok := haystack[needle]; !ok {
				return false
			}
		}
		return true
	}
	anyMigrated := false
	for g := range removed {
		if members, ok := storedGroupMembers[g]; ok && len(members) > 0 && subsetOf(members, newMembers) {
			anyMigrated = true
			break
		}
	}
	anySplitNew := false
	for g := range added {
		if members, ok := newGroupMembers[g]; ok && len(members) > 0 && subsetOf(members, storedMembers) {
			anySplitNew = true
			break
		}
	}
	groups := map[string]struct{}{}
	for g := range changedGroups {
		groups[g] = struct{}{}
	}
	for g := range retiredGroups {
		groups[g] = struct{}{}
	}
	var outcome RekeyOutcome
	switch {
	case len(added) == 0 && len(removed) == 0:
		if !memberDiff {
			return silent
		}
		outcome = RekeyOutcomeMemberOnly
	case len(removed) > 0 && len(added) == 0:
		if anyMigrated {
			outcome = RekeyOutcomeMerge
		} else {
			outcome = RekeyOutcomeRetireOnly
		}
	case len(added) > 0 && len(removed) == 0:
		if !anySplitNew {
			return silent
		}
		outcome = RekeyOutcomeSplit
	default:
		switch {
		case anySplitNew:
			outcome = RekeyOutcomeSplit
		case anyMigrated:
			outcome = RekeyOutcomeMerge
		default:
			outcome = RekeyOutcomeRetireOnly
		}
	}
	if outcome == RekeyOutcomeSplit {
		groupOf := func(cveID, advisoryID string) string {
			return coalescedFindingGroupID(cveID, advisoryID)
		}
		for g := range added {
			if members, ok := newGroupMembers[g]; ok && len(members) > 0 && subsetOf(members, storedMembers) {
				groups[g] = struct{}{}
			}
		}
		for _, row := range rows {
			g := groupOf(row.cveID, row.advisoryID)
			var has bool
			if rerun {
				_, has = own[row.factID]
			} else {
				_, has = prior[row.stableKey]
			}
			if has {
				continue
			}
			if members, ok := newGroupMembers[g]; ok && len(members) > 0 && subsetOf(members, storedMembers) {
				groups[g] = struct{}{}
				findings++
			}
		}
	}
	return RekeyReport{
		Outcome:    outcome,
		Groups:     sortedSetKeys(groups),
		Findings:   findings,
		Tombstoned: tombstoned,
		Count:      true,
	}
}

// differenceOfSets returns the ids in have that missing lacks.
func differenceOfSets(have, missing map[string]struct{}) map[string]struct{} {
	out := map[string]struct{}{}
	for id := range have {
		if _, ok := missing[id]; !ok {
			out[id] = struct{}{}
		}
	}
	return out
}
