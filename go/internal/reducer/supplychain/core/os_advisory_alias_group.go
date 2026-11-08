// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Alias-connected advisory groups (#10407, arbiter ruling 9209-3-2).
//
// One connected component of the undirected graph whose nodes are advisory
// ids and CVE ids and whose edges are the typed `aliases` lists of loaded
// vulnerability.cve records (record id <-> each alias) yields one finding
// per package, keyed by the lowest CVE id of the component (else the lowest
// advisory id, compared in Go byte order). `upstream` keeps its #9914 filing
// behavior and never merges groups: this pass runs after
// groupOSAdvisoriesByUpstream and reads native (non-UpstreamCopy) records
// only, so one DSA can never bridge the CVEs it covers into one component
// through its own copies.
//
// Records join components through AdvisoryID, never through cve_id: an alias
// can give unrelated records one cve_id, and that coincidence must not move
// packages (#9914 rule, applied to alias edges).
const (
	// supplyChainAliasRecordCap bounds the aliases one record may carry. A
	// record naming more is quarantined: singleton, no edges, still
	// aliasable. A DSA that bundled its CVEs as aliases instead of upstream
	// edges trips this cap, which is the point (upstream is the bulletin
	// edge; aliases are the identity edge).
	supplyChainAliasRecordCap = 5
	// supplyChainAliasGroupCap bounds the member ids of one component. A
	// larger component is quarantined: every member falls back to its
	// per-record key with a counted marker. A group id over a partial
	// component is a wrong stable key, so the pass never emits one.
	supplyChainAliasGroupCap = 32
)

// MissingEvidence markers for quarantine fallback. A quarantined finding
// keeps its per-record key (today's FirstNonBlank(cve_id, advisory_id)
// identity) with one of these markers instead of a partial-component group
// id, so a later, fuller pass renames nothing without an upstream alias
// change.
const (
	// supplyChainMissingAdvisoryGroupRecordCap marks a finding whose record
	// exceeded the per-record alias cap.
	supplyChainMissingAdvisoryGroupRecordCap = "advisory group record alias cap exceeded"
	// supplyChainMissingAdvisoryGroupSizeCap marks a finding whose component
	// exceeded the component size cap.
	supplyChainMissingAdvisoryGroupSizeCap = "advisory group component size cap exceeded"
)

// Result label values for
// eshu_dp_supply_chain_impact_os_advisory_alias_groups_total, the closed set
// of its result label (#10407).
const (
	// aliasGroupResultGrouped: the record joined a multi-record component
	// and is indexed under the component's group id.
	aliasGroupResultGrouped = "grouped"
	// aliasGroupResultUngrouped: the record's component is its own
	// singleton, so its key is unchanged.
	aliasGroupResultUngrouped = "ungrouped"
	// aliasGroupResultRecordCap: the record exceeded the alias cap and
	// keeps its per-record key with a marker.
	aliasGroupResultRecordCap = "quarantined_record_cap"
	// aliasGroupResultGroupCap: the record's component exceeded the size
	// cap and every member keeps its per-record key with a marker.
	aliasGroupResultGroupCap = "quarantined_group_cap"
	// aliasGroupResultDenied: the record or an alias target is deny-listed
	// and the record is dropped entirely.
	aliasGroupResultDenied = "denied"
)

// Success-log sub-signal names for the alias grouping split. They sum the
// counter over passes, so an operator without a metrics backend sees whether
// alias edges group records on this pass at all.
const (
	aliasAdvisoryGroupsSubSignal            = "advisory_groups"
	aliasAdvisoryGroupsQuarantinedSubSignal = "advisory_groups_quarantined"
)

// supplyChainAliasGroupConfig carries the operator lists and the
// truncated-load kill switch for one grouping pass. The lists ship empty;
// populating them is an operator act. Allow exempts a record from the
// record cap and a group from the group cap when any member id is listed.
// Deny drops a record naming a listed id and drops alias edges to a listed
// id entirely.
type supplyChainAliasGroupConfig struct {
	// disabled is true when the evidence load stopped short (rounds or
	// budget truncation): the alias graph may be partial, so the pass must
	// not group-key anything and every record keeps its per-record key.
	disabled bool
	allow    map[string]struct{}
	deny     map[string]struct{}
}

// supplyChainAliasGroupConfigFromLists builds the pass config from the
// handler's operator lists. Empty lists stay nil maps; membership tests
// read them read-only.
func supplyChainAliasGroupConfigFromLists(allow, deny []string) supplyChainAliasGroupConfig {
	return supplyChainAliasGroupConfig{allow: aliasGroupStringSet(allow), deny: aliasGroupStringSet(deny)}
}

func aliasGroupStringSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out[trimmed] = struct{}{}
		}
	}
	return out
}

// supplyChainAliasGroupKey is one cell of the alias grouping counter.
type supplyChainAliasGroupKey struct {
	result string
}

// supplyChainAliasGroup is the pass outcome for one index key: either a
// grouped component (members plus enrichment provenance) or a quarantined
// singleton (marker, no members).
type supplyChainAliasGroup struct {
	// id is the component's group id: lowest CVE else lowest advisory id.
	id string
	// members is the sorted member-id set of the component, nil unless the
	// component grouped (item 3.3/3.4 input).
	members []string
	// grouped reports a multi-record component or a component joined by a
	// non-self alias edge. Only grouped findings carry members.
	grouped bool
	// missingEvidence is the quarantine marker for fallback findings, ""
	// on grouped and ungrouped keys.
	missingEvidence string
	// epssSource names the member CVE id that supplied the group's highest
	// EPSS probability, "" when no member CVE carries one.
	epssSource string
	// kevSource names a member CVE id in KEV, "" when no member is in KEV.
	kevSource string
}

// supplyChainAliasGroupStats is what the grouping pass leaves on the index
// for the pass's counters and success-log sub-signals.
type supplyChainAliasGroupStats struct {
	// cells counts examined records by result.
	cells map[supplyChainAliasGroupKey]int
	// groups counts grouped components.
	groups int
	// sizes holds one member count per grouped component for the
	// group-size histogram.
	sizes []int
	// compute is the pass wall time for the duration histogram.
	compute time.Duration
}

// aliasGroupUnionFind is a path-compressed, union-by-rank disjoint set over
// interned integer ids. Callers intern each advisory and CVE id once, so
// the hot union/find loop pays slice indexes instead of string hashing.
type aliasGroupUnionFind struct {
	parent []int
	rank   []int8
}

func (u *aliasGroupUnionFind) add() int {
	id := len(u.parent)
	u.parent = append(u.parent, id)
	u.rank = append(u.rank, 0)
	return id
}

func (u *aliasGroupUnionFind) find(id int) int {
	root := id
	for u.parent[root] != root {
		root = u.parent[root]
	}
	for u.parent[id] != root {
		u.parent[id], id = root, u.parent[id]
	}
	return root
}

func (u *aliasGroupUnionFind) union(a, b int) {
	ra, rb := u.find(a), u.find(b)
	if ra == rb {
		return
	}
	if u.rank[ra] < u.rank[rb] {
		ra, rb = rb, ra
	}
	u.parent[rb] = ra
	if u.rank[ra] == u.rank[rb] {
		u.rank[ra]++
	}
}

// lowestAdvisoryGroupID returns the group id for a component's member ids:
// the lowest CVE id, else the lowest advisory id, compared in Go byte order
// (so CVE-2021-9999 sorts after CVE-2021-10000; any SQL min() over the same
// set must pin COLLATE "C" or defer to this computation).
func lowestAdvisoryGroupID(members []string) string {
	var cve, other string
	for _, id := range members {
		if _, ok := upstreamCVEKey(id); ok {
			if cve == "" || id < cve {
				cve = id
			}
			continue
		}
		if other == "" || id < other {
			other = id
		}
	}
	if cve != "" {
		return cve
	}
	return other
}

// aliasGroupRecord is one advisory record (all native observations sharing an
// AdvisoryID) as the grouping pass sees it. outcome carries the record's
// fate once classified; cveIDs keeps the pre-relabel keys, which the
// package/product move reads as the pairing set.
type aliasGroupRecord struct {
	advisoryID string
	cveIDs     []string
	aliases    []string
	withdrawn  bool
	allowed    bool
	outcome    aliasRecordOutcome
}

// groupOSAdvisoriesByAlias unions native advisory records over symmetric
// aliases edges and rekeys grouped records to their component's group id.
// Quarantined records keep per-record keys with markers; denied records are
// dropped; a disabled pass (truncated load) changes nothing.
func groupOSAdvisoriesByAlias(index *supplyChainImpactIndex, config supplyChainAliasGroupConfig) {
	started := time.Now()
	stats := supplyChainAliasGroupStats{cells: map[supplyChainAliasGroupKey]int{}}
	defer func() {
		stats.compute = time.Since(started)
		index.aliasStats = stats
	}()
	if config.disabled || len(index.cves) == 0 {
		return
	}
	records := aliasGroupRecords(index.cves, config)
	if len(records) == 0 {
		return
	}
	// Intern each id once: the union loop below then pays slice indexes
	// instead of string hashing. ids[internal] round-trips back to the id
	// string for the component and group steps.
	ids := make([]string, 0, 2*len(records))
	internal := make(map[string]int, 2*len(records))
	sets := &aliasGroupUnionFind{
		parent: make([]int, 0, 2*len(records)),
		rank:   make([]int8, 0, 2*len(records)),
	}
	// intern assigns each id the next set index and grows the set with it,
	// so every interned id is a live node even when it carries no edge
	// (quarantined and withdrawn records intern but never union).
	intern := func(id string) int {
		if n, ok := internal[id]; ok {
			return n
		}
		n := sets.add()
		ids = append(ids, id)
		internal[id] = n
		return n
	}
	quarantinedRecord := make(map[string]bool, len(records))
	for _, record := range records {
		if aliasRecordDenied(record, config) {
			continue
		}
		anchor := intern(record.advisoryID)
		for _, id := range record.cveIDs {
			if id != "" {
				intern(id)
			}
		}
		if len(record.aliases) > supplyChainAliasRecordCap && !record.allowed {
			quarantinedRecord[record.advisoryID] = true
			continue
		}
		if record.withdrawn {
			continue
		}
		for _, id := range record.cveIDs {
			if id == "" {
				continue
			}
			sets.union(anchor, internal[id])
		}
		for _, alias := range record.aliases {
			if _, denied := config.deny[alias]; denied {
				continue
			}
			sets.union(anchor, intern(alias))
		}
	}
	components := make(map[int][]string, len(ids))
	for n, id := range ids {
		root := sets.find(n)
		components[root] = append(components[root], id)
	}
	recordRoot := make(map[string]int, len(records))
	for _, record := range records {
		if aliasRecordDenied(record, config) {
			continue
		}
		recordRoot[record.advisoryID] = sets.find(internal[record.advisoryID])
	}
	groupID := make(map[int]string, len(components))
	groupAllowed := make(map[int]bool, len(components))
	for root, members := range components {
		sort.Strings(members)
		components[root] = members
		groupID[root] = lowestAdvisoryGroupID(members)
		for _, id := range members {
			if _, ok := config.allow[id]; ok {
				groupAllowed[root] = true
				break
			}
		}
	}
	quarantinedGroup := make(map[int]bool, len(components))
	for root, members := range components {
		if len(members) > supplyChainAliasGroupCap && !groupAllowed[root] {
			quarantinedGroup[root] = true
		}
	}
	applyAliasGroups(index, records, recordRoot, components, groupID, quarantinedGroup, quarantinedRecord, config, &stats)
}

// emitAliasAdvisoryGroups records one counter point per non-zero (result)
// cell, one group-size histogram point per grouped component, and the pass
// compute duration.
func (h SupplyChainImpactHandler) emitAliasAdvisoryGroups(ctx context.Context, stats supplyChainImpactBuildStats) {
	if h.Instruments == nil {
		return
	}
	if h.Instruments.SupplyChainImpactOSAdvisoryAliasGroups != nil {
		for key, count := range stats.aliasGroups {
			if count == 0 {
				continue
			}
			h.Instruments.SupplyChainImpactOSAdvisoryAliasGroups.Add(ctx, int64(count), metric.WithAttributes(
				telemetry.AttrResult(key.result),
			))
		}
	}
	if h.Instruments.SupplyChainImpactAdvisoryGroupSize != nil {
		for _, size := range stats.aliasGroupSizes {
			h.Instruments.SupplyChainImpactAdvisoryGroupSize.Record(ctx, float64(size))
		}
	}
	if h.Instruments.SupplyChainImpactAdvisoryGroupComputeDuration != nil && stats.aliasGroupCompute > 0 {
		h.Instruments.SupplyChainImpactAdvisoryGroupComputeDuration.Record(ctx, stats.aliasGroupCompute.Seconds())
	}
}

// aliasRecordDenied reports whether a deny-listed id names the record: its
// advisory id or any of its CVE ids.
func aliasRecordDenied(record aliasGroupRecord, config supplyChainAliasGroupConfig) bool {
	if _, ok := config.deny[record.advisoryID]; ok {
		return true
	}
	for _, id := range record.cveIDs {
		if _, ok := config.deny[id]; ok {
			return true
		}
	}
	return false
}

// aliasGroupRecords folds native observations into advisory records keyed by
// AdvisoryID. Upstream copies contribute no alias edges, so one DSA can never
// bridge the CVEs it covers into one component through its own copies.
func aliasGroupRecords(cves []supplychainmodel.ImpactCVE, config supplyChainAliasGroupConfig) []aliasGroupRecord {
	// Index into records, not pointers: taking a new address per observation
	// escapes to the heap on every record.
	byAdvisory := make(map[string]int, len(cves))
	records := make([]aliasGroupRecord, 0, len(cves))
	for i := range cves {
		cve := &cves[i]
		if cve.UpstreamCopy || cve.AdvisoryID == "" {
			continue
		}
		idx, ok := byAdvisory[cve.AdvisoryID]
		if !ok {
			records = append(records, aliasGroupRecord{advisoryID: cve.AdvisoryID, withdrawn: true})
			idx = len(records) - 1
			byAdvisory[cve.AdvisoryID] = idx
		}
		if cve.CVEID != "" && !aliasGroupContainsString(records[idx].cveIDs, cve.CVEID) {
			records[idx].cveIDs = append(records[idx].cveIDs, cve.CVEID)
		}
		for _, alias := range cve.Aliases {
			if alias != "" && !aliasGroupContainsString(records[idx].aliases, alias) {
				records[idx].aliases = append(records[idx].aliases, alias)
			}
		}
		if !impactCVERetracted(*cve) {
			records[idx].withdrawn = false
		}
		if _, ok := config.allow[cve.AdvisoryID]; ok {
			records[idx].allowed = true
		}
		if _, ok := config.allow[cve.CVEID]; ok {
			records[idx].allowed = true
		}
	}
	return records
}

func aliasGroupContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
