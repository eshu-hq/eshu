// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"sort"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// aliasRecordOutcome is one record's grouping fate: the group id a grouped
// record rekeys to, or a quarantine marker for a fallback record. The zero
// value keeps the per-record key (trivial singletons move nothing).
type aliasRecordOutcome struct {
	group   string
	grouped bool
	marker  string
}

// applyAliasGroups drops denied records, rekeys grouped observations to
// their component's group id, moves their packages and products, merges
// group risk signals, and records the per-key outcome for the finding
// constructors.
func applyAliasGroups(
	index *supplyChainImpactIndex,
	records []aliasGroupRecord,
	recordRoot map[string]int,
	components map[int][]string,
	groupID map[int]string,
	quarantinedGroup map[int]bool,
	quarantinedRecord map[string]bool,
	config supplyChainAliasGroupConfig,
	stats *supplyChainAliasGroupStats,
) {
	count := func(result string) {
		stats.cells[supplyChainAliasGroupKey{result: result}]++
	}
	// Record indexes keyed by advisory id for the package/product move
	// below. Indexes keep the maps small: records carry alias slices a
	// value map would copy per insert.
	byAdvisory := make(map[string]int, len(records))
	for i := range records {
		byAdvisory[records[i].advisoryID] = i
	}
	// A denied record and everything keyed to it leaves the index: the
	// record contributes no observation, package, product, or edge. A
	// recordless observation (empty AdvisoryID, so aliasGroupRecords built
	// no record for it) keeps its place unless it names a listed id
	// itself, so deny never sweeps recordless evidence outright. The lists
	// ship empty, so a pass with no denials keeps the index as is.
	if len(config.deny) > 0 {
		kept := make([]supplychainmodel.ImpactCVE, 0, len(index.cves))
		for _, cve := range index.cves {
			if cve.UpstreamCopy {
				kept = append(kept, cve)
				continue
			}
			if idx, ok := byAdvisory[cve.AdvisoryID]; ok {
				if aliasRecordDenied(records[idx], config) {
					continue
				}
			} else if aliasObservationDenied(cve, config) {
				continue
			}
			kept = append(kept, cve)
		}
		index.cves = kept
		dropAliasDeniedEvidence(index, records, byAdvisory, config)
	}
	// Classify every record in place: its outcome lives on the record, so
	// the relabel and move steps below read one advisory->index lookup and
	// no outcome map. Component membership is root -> record indexes.
	componentRecords := make(map[int][]int, len(records))
	for i := range records {
		if len(config.deny) > 0 && aliasRecordDenied(records[i], config) {
			count(aliasGroupResultDenied)
			continue
		}
		root := recordRoot[records[i].advisoryID]
		componentRecords[root] = append(componentRecords[root], i)
	}
	stats.sizes = make([]int, 0, len(componentRecords))
	for root, members := range componentRecords {
		isGrouped, result := aliasComponentOutcome(records, members, quarantinedGroup[root], quarantinedRecord)
		if isGrouped {
			stats.groups++
			stats.sizes = append(stats.sizes, len(components[root]))
		}
		for _, i := range members {
			switch result {
			case aliasGroupResultRecordCap:
				count(aliasGroupResultRecordCap)
				records[i].outcome = aliasRecordOutcome{marker: supplyChainMissingAdvisoryGroupRecordCap}
			case aliasGroupResultGroupCap:
				count(aliasGroupResultGroupCap)
				records[i].outcome = aliasRecordOutcome{marker: supplyChainMissingAdvisoryGroupSizeCap}
			case aliasGroupResultGrouped:
				count(aliasGroupResultGrouped)
				records[i].outcome = aliasRecordOutcome{group: groupID[root], grouped: true}
			default:
				count(aliasGroupResultUngrouped)
			}
		}
	}
	// Rekey grouped observations, then move the packages and products that
	// paired with them (matched by advisory id under one of the record's
	// pre-relabel keys in records cveIDs, the #9914 join rule) under the
	// group key.
	for i := range index.cves {
		cve := &index.cves[i]
		if cve.UpstreamCopy {
			continue
		}
		idx, ok := byAdvisory[cve.AdvisoryID]
		if !ok {
			continue
		}
		if group := records[idx].outcome.group; group != "" {
			cve.CVEID = group
		}
	}
	// Grouped roots and their group ids, derived once for the merge and
	// outcome steps below.
	grouped := make(map[int]string, len(componentRecords))
	for root, members := range componentRecords {
		for _, i := range members {
			if records[i].outcome.grouped {
				grouped[root] = groupID[root]
				break
			}
		}
	}
	moveAliasGroupedPackages(index.affectedPackages, records, byAdvisory)
	if len(index.affectedProducts) > 0 {
		moveAliasGroupedProducts(index.affectedProducts, records)
	}
	sources := mergeAliasGroupRiskSignals(index, components, grouped)
	recordAliasGroupOutcomes(index, records, components, grouped, sources)
}

// aliasComponentOutcome classifies one component: grouped with its group id,
// quarantined with a cause, or ungrouped. A component groups when it holds
// more than one record or a record aliases an id outside its own id set
// (an absent alias target still names the same advisory). A withdrawn
// singleton never groups on its own aliases: they contributed no edges
// (groupOSAdvisoriesByAlias unions around withdrawn records), so it stays
// ungrouped unless another record joined its root, which would have made it
// a multi-record component above.
func aliasComponentOutcome(
	records []aliasGroupRecord,
	members []int,
	groupQuarantined bool,
	quarantinedRecord map[string]bool,
) (grouped bool, result string) {
	for _, i := range members {
		if quarantinedRecord[records[i].advisoryID] {
			return false, aliasGroupResultRecordCap
		}
	}
	if groupQuarantined {
		return false, aliasGroupResultGroupCap
	}
	if len(members) > 1 {
		return true, aliasGroupResultGrouped
	}
	if records[members[0]].withdrawn {
		return false, aliasGroupResultUngrouped
	}
	first := records[members[0]]
	own := map[string]struct{}{first.advisoryID: {}}
	for _, id := range first.cveIDs {
		own[id] = struct{}{}
	}
	for _, alias := range first.aliases {
		if _, isOwn := own[alias]; !isOwn {
			return true, aliasGroupResultGrouped
		}
	}
	return false, aliasGroupResultUngrouped
}

// dropAliasDeniedEvidence removes the packages and products a denied record
// named: a deny-listed record contributes nothing to any finding. Packages
// drop by advisory id; products, which carry none, drop by key, except keys
// a kept record still names.
func dropAliasDeniedEvidence(
	index *supplyChainImpactIndex,
	records []aliasGroupRecord,
	byAdvisory map[string]int,
	config supplyChainAliasGroupConfig,
) {
	deniedAdvisory := make(map[string]struct{})
	for advisoryID, i := range byAdvisory {
		if aliasRecordDenied(records[i], config) {
			deniedAdvisory[advisoryID] = struct{}{}
		}
	}
	if len(deniedAdvisory) == 0 {
		return
	}
	for key, pkgs := range index.affectedPackages {
		kept := pkgs[:0]
		for _, pkg := range pkgs {
			if _, denied := deniedAdvisory[pkg.AdvisoryID]; !denied {
				kept = append(kept, pkg)
			}
		}
		if len(kept) == 0 {
			delete(index.affectedPackages, key)
			continue
		}
		index.affectedPackages[key] = kept
	}
	// Products carry no advisory id, so they drop by key: a key every
	// record naming it has denied loses its products. A key a kept record
	// still names keeps them — the finding under that key is the kept
	// record's, and clearing shared evidence would delete live findings.
	deniedKeys := make(map[string]struct{})
	keptKeys := make(map[string]struct{})
	for i := range records {
		keys := keptKeys
		if aliasRecordDenied(records[i], config) {
			keys = deniedKeys
		}
		if records[i].advisoryID != "" {
			keys[records[i].advisoryID] = struct{}{}
		}
		for _, key := range records[i].cveIDs {
			if key != "" {
				keys[key] = struct{}{}
			}
		}
	}
	for key := range deniedKeys {
		if _, kept := keptKeys[key]; kept {
			continue
		}
		delete(index.affectedProducts, key)
	}
}

// moveAliasGroupedPackages files the packages that paired with a relabeled
// record under the group key. A package moves only when it names the
// record's advisory id and sits under one of the record's old keys, so a
// package that happens to share a key with another record's component stays
// where the decode put it.
func moveAliasGroupedPackages(
	packages map[string][]supplychainmodel.AffectedPackage,
	records []aliasGroupRecord,
	byAdvisory map[string]int,
) {
	// Single pass with no move buffer: a moved package lands under its group
	// key immediately. Landing in a not-yet-visited key is safe — a package
	// already keyed under its outcome group hits the outcome.group == key
	// keep branch when that key is visited, so it is never moved twice.
	for key, pkgs := range packages {
		kept := pkgs[:0]
		for _, pkg := range pkgs {
			idx, ok := byAdvisory[pkg.AdvisoryID]
			if !ok {
				kept = append(kept, pkg)
				continue
			}
			outcome := records[idx].outcome
			if !outcome.grouped || outcome.group == key {
				kept = append(kept, pkg)
				continue
			}
			if !aliasGroupContainsString(records[idx].cveIDs, key) {
				kept = append(kept, pkg)
				continue
			}
			pkg.CVEID = outcome.group
			packages[outcome.group] = append(packages[outcome.group], pkg)
		}
		if len(kept) == 0 {
			delete(packages, key)
			continue
		}
		packages[key] = kept
	}
}

// moveAliasGroupedProducts files the affected products that paired with a
// relabeled record under the group key. AffectedProduct carries no advisory
// id, so a product moves only when every grouped record that owned its key
// agrees on one group; a key naming records bound for different groups is
// ambiguous and stays put rather than joining the wrong one.
func moveAliasGroupedProducts(
	products map[string][]supplychainmodel.AffectedProduct,
	records []aliasGroupRecord,
) {
	// Invert once: old key -> the one group every owning record agrees on.
	// A key naming records bound for different groups is ambiguous and its
	// products stay put rather than joining the wrong one.
	groupsByOldKey := make(map[string]string)
	ambiguous := make(map[string]struct{})
	for i := range records {
		outcome := records[i].outcome
		if !outcome.grouped || outcome.group == "" {
			continue
		}
		for _, key := range records[i].cveIDs {
			if prior, seen := groupsByOldKey[key]; seen {
				if prior != outcome.group {
					ambiguous[key] = struct{}{}
				}
				continue
			}
			groupsByOldKey[key] = outcome.group
		}
	}
	for key, prods := range products {
		group, owned := groupsByOldKey[key]
		if !owned || group == key {
			continue
		}
		if _, isAmbiguous := ambiguous[key]; isAmbiguous {
			continue
		}
		moved := make([]supplychainmodel.AffectedProduct, 0, len(prods))
		for _, product := range prods {
			product.CVEID = group
			moved = append(moved, product)
		}
		delete(products, key)
		products[group] = append(products[group], moved...)
	}
}

// aliasGroupEnrichment names the member CVE ids that supplied a grouped
// component's merged risk signals.
type aliasGroupEnrichment struct {
	epssSource string
	kevSource  string
}

// mergeAliasGroupRiskSignals folds every grouped component's per-member risk
// signals into one entry under the group id, so findings the pass rekeyed
// read the group's highest EPSS and KEV flag instead of one member's. The
// group takes the highest EPSS probability over its member CVE ids (ties
// break to the lowest member id) and is KEV-flagged when any member CVE is
// in KEV (naming the lowest such member), with provenance naming the
// supplying member for items 3.3 and 3.4.
func mergeAliasGroupRiskSignals(
	index *supplyChainImpactIndex,
	components map[int][]string,
	grouped map[int]string,
) map[int]aliasGroupEnrichment {
	sources := make(map[int]aliasGroupEnrichment, len(grouped))
	// No member signals: every group keeps empty enrichment without paying
	// a lookup per member id.
	if len(index.riskSignals) == 0 {
		for root := range grouped {
			sources[root] = aliasGroupEnrichment{}
		}
		return sources
	}
	for root, members := range components {
		group, isGrouped := grouped[root]
		if !isGrouped {
			continue
		}
		var (
			bestEPS      float64 = -1
			bestEPSValid bool
			epssSource   string
			kevSource    string
			merged       supplychainmodel.RiskSignals
		)
		for _, id := range members {
			signals, ok := index.riskSignals[id]
			if !ok {
				continue
			}
			if signals.EPSSProbability != "" {
				if value, err := strconv.ParseFloat(signals.EPSSProbability, 64); err == nil {
					if !bestEPSValid || value > bestEPS || (value == bestEPS && id < epssSource) {
						bestEPS, bestEPSValid = value, true
						epssSource = id
						merged.EPSSFactID = signals.EPSSFactID
						merged.EPSSProbability = signals.EPSSProbability
						merged.EPSSPercentile = signals.EPSSPercentile
						merged.EPSSScoreDate = signals.EPSSScoreDate
						merged.EPSSModelVersion = signals.EPSSModelVersion
						merged.EPSSObservedAt = signals.EPSSObservedAt
						merged.EPSSSelected = signals.EPSSSelected
					}
				}
			}
			if signals.KnownExploited && (kevSource == "" || id < kevSource) {
				kevSource = id
				merged.KEVFactID = signals.KEVFactID
				merged.KnownExploited = true
			}
		}
		// An empty merge reads identically to a missing key, so only a
		// group with member signals pays the map insert.
		if merged != (supplychainmodel.RiskSignals{}) {
			index.riskSignals[group] = merged
		}
		sources[root] = aliasGroupEnrichment{epssSource: epssSource, kevSource: kevSource}
	}
	return sources
}

// recordAliasGroupOutcomes stores the per-key pass outcome the finding
// constructors read: grouped entries under the group id (members plus
// enrichment provenance), quarantine entries under each fallback key
// (marker, no members). A key holding both a grouped entry and a quarantine
// fallback resolves to the grouped entry: the finding for that key is the
// group's, complete, and carries no marker.
func recordAliasGroupOutcomes(
	index *supplyChainImpactIndex,
	records []aliasGroupRecord,
	components map[int][]string,
	grouped map[int]string,
	sources map[int]aliasGroupEnrichment,
) {
	if index.aliasGroups == nil {
		index.aliasGroups = make(map[string]supplyChainAliasGroup, len(records))
	}
	// Grouped entries first: a key holding both a grouped entry and a
	// quarantine fallback resolves to the grouped entry.
	for root, id := range grouped {
		members := components[root]
		enrichment := sources[root]
		if _, exists := index.aliasGroups[id]; exists {
			continue
		}
		index.aliasGroups[id] = supplyChainAliasGroup{
			id:              id,
			members:         members,
			grouped:         true,
			epssSource:      enrichment.epssSource,
			kevSource:       enrichment.kevSource,
			missingEvidence: "",
		}
	}
	// Quarantine fallbacks in advisory order, so the rare key shared by two
	// quarantined records resolves deterministically. Only records with a
	// marker pay the sort.
	marked := make([]int, 0)
	for i := range records {
		if records[i].outcome.marker != "" {
			marked = append(marked, i)
		}
	}
	sort.Slice(marked, func(a, b int) bool { return records[marked[a]].advisoryID < records[marked[b]].advisoryID })
	for _, i := range marked {
		record := records[i]
		keys := append(append([]string{}, record.cveIDs...), record.advisoryID)
		for _, key := range keys {
			if key == "" {
				continue
			}
			if _, grouped := index.aliasGroups[key]; grouped {
				continue
			}
			index.aliasGroups[key] = supplyChainAliasGroup{id: key, missingEvidence: record.outcome.marker}
		}
	}
}
