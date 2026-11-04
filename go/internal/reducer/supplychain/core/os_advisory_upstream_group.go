// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"slices"
	"sort"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Result label values for
// eshu_dp_supply_chain_impact_os_advisory_upstream_groups_total, the closed
// set of its result label (#9914).
const (
	// osUpstreamGrouped: the record names at least one CVE in its upstream
	// list that differs from the group key it already had, so the index files
	// it under that CVE.
	osUpstreamGrouped = "grouped"
	// osUpstreamUngrouped: the record has an OS vendor id or an upstream
	// list but no usable CVE key, so it keeps its own group.
	osUpstreamUngrouped = "ungrouped"
)

// Success-log sub-signal names for the grouping split. They sum the counter
// over sources, so an operator without a metrics backend sees whether the
// producer's upstream lists reach the reducer at all.
const (
	osAdvisoriesGroupedSubSignal   = "os_advisories_grouped"
	osAdvisoriesUngroupedSubSignal = "os_advisories_ungrouped"
)

// osUpstreamGroupKey is one cell of the grouping counter.
type osUpstreamGroupKey struct {
	source string
	result string
}

// osUpstreamGrouping is what the grouping pass leaves on the index for the
// pass's counters.
type osUpstreamGrouping struct {
	// cells counts the examined vulnerability.cve records by source and result.
	cells map[osUpstreamGroupKey]int
	// rekeyed is true when the pass filed an affected_package fact under a CVE
	// key as well as under the key it arrived with. The OS join census reads
	// it to count each fact once instead of once per group it joined.
	rekeyed bool
}

func (g *osUpstreamGrouping) count(source, result string) {
	if g.cells == nil {
		g.cells = make(map[osUpstreamGroupKey]int, 4)
	}
	g.cells[osUpstreamGroupKey{source: source, result: result}]++
}

// osAdvisoriesWithGrouping returns how many examined records carry result.
func (s supplyChainImpactBuildStats) osAdvisoriesWithGrouping(result string) int {
	total := 0
	for key, count := range s.osUpstreamGroups {
		if key.result == result {
			total += count
		}
	}
	return total
}

// groupOSAdvisoriesByUpstream files every OSV distro record under the CVE ids
// its upstream list names (#9914). A Debian, Ubuntu or Alpine record is
// published under the distro's own id (DEBIAN-CVE-X, DSA-N-1, USN-N-1,
// ALPINE-CVE-X) and names the CVEs it covers only in `upstream`, so the NVD
// and GHSA records for CVE-X and the distro records for CVE-X would otherwise
// each read as their own finding. After this pass the cve observations and the
// affected_package facts of each such record are also indexed under the CVE
// key, so the classifier sees one group per CVE.
//
// The pass runs once, after every envelope is indexed, so it does not depend
// on whether a record's affected_package facts arrive before or after its cve
// fact. It reads only the typed Upstream field, never the raw payload, and it
// does not change what the producer stores: the producer's cve_id is
// untouched, and a record with no usable CVE key keeps the group it had.
//
// The join is on AdvisoryID, not cve_id: a record whose alias gives it the same
// cve_id as an unrelated record must not carry that record's packages to the
// CVEs its own upstream list names. Two cve facts that share an AdvisoryID are
// one record, so if either names an upstream CVE both follow it. A record with
// no AdvisoryID cannot be joined to its packages, so it fails closed: counted
// ungrouped, nothing copied. The typed decode requires the field, so production
// never reaches that branch.
//
// A record with N CVE keys is a member of N groups (a bulletin covering N CVEs
// reads one finding per CVE). Keys are not closed transitively, so a cycle of
// records naming each other terminates after one step.
//
// A record that is not an OS vendor record and has no upstream list costs
// the prefix comparisons in osAdvisoryUpstreamSource and no allocation.
func groupOSAdvisoriesByUpstream(index *supplyChainImpactIndex) {
	var keysByAdvisory map[string][]string
	for _, cve := range index.cves {
		source := osAdvisoryUpstreamSource(cve)
		if source == "" {
			continue
		}
		if cve.AdvisoryID == "" || !hasUpstreamCVEKey(cve) {
			index.osUpstream.count(source, osUpstreamUngrouped)
			continue
		}
		index.osUpstream.count(source, osUpstreamGrouped)
		if keysByAdvisory == nil {
			keysByAdvisory = make(map[string][]string, 4)
		}
		for _, key := range upstreamGroupKeys(cve) {
			if !slices.Contains(keysByAdvisory[cve.AdvisoryID], key) {
				keysByAdvisory[cve.AdvisoryID] = append(keysByAdvisory[cve.AdvisoryID], key)
			}
		}
	}
	if keysByAdvisory == nil {
		return
	}
	// A sibling cve fact of a grouped record keeps its own CVE key even when it
	// carries no upstream list. It cannot join the map in the loop above without
	// depending on envelope order.
	for _, cve := range index.cves {
		keys, grouped := keysByAdvisory[cve.AdvisoryID]
		if _, isCVE := upstreamCVEKey(cve.CVEID); grouped && isCVE && !slices.Contains(keys, cve.CVEID) {
			keysByAdvisory[cve.AdvisoryID] = append(keys, cve.CVEID)
		}
	}

	// Snapshot first: a CVE key can itself be another record's join key, and a
	// copy must come from the facts that arrived, not from an earlier copy.
	type recordBucket struct{ advisoryID, joinKey string }
	visited := make(map[recordBucket]struct{}, len(keysByAdvisory))
	buckets := make([]recordBucket, 0, len(keysByAdvisory))
	additions := map[string][]supplychainmodel.AffectedPackage{}
	regrouped := make([]supplychainmodel.ImpactCVE, 0, len(index.cves)+len(keysByAdvisory))
	for _, cve := range index.cves {
		keys, grouped := keysByAdvisory[cve.AdvisoryID]
		if !grouped {
			regrouped = append(regrouped, cve)
			continue
		}
		for _, key := range keys {
			copied := cve
			copied.CVEID = key
			regrouped = append(regrouped, copied)
		}
		bucket := recordBucket{advisoryID: cve.AdvisoryID, joinKey: cve.CVEID}
		if _, seen := visited[bucket]; seen {
			continue
		}
		visited[bucket] = struct{}{}
		buckets = append(buckets, bucket)
		for _, pkg := range index.affectedPackages[bucket.joinKey] {
			if pkg.AdvisoryID != bucket.advisoryID {
				continue
			}
			for _, key := range keys {
				if key == bucket.joinKey {
					continue
				}
				copied := pkg
				copied.CVEID = key
				additions[key] = append(additions[key], copied)
				index.osUpstream.rekeyed = true
			}
		}
	}
	for _, bucket := range buckets {
		if slices.Contains(keysByAdvisory[bucket.advisoryID], bucket.joinKey) {
			continue
		}
		remaining := slices.DeleteFunc(index.affectedPackages[bucket.joinKey], func(pkg supplychainmodel.AffectedPackage) bool {
			return pkg.AdvisoryID == bucket.advisoryID
		})
		if len(remaining) == 0 {
			delete(index.affectedPackages, bucket.joinKey)
			continue
		}
		index.affectedPackages[bucket.joinKey] = remaining
	}
	for key, added := range additions {
		index.affectedPackages[key] = append(index.affectedPackages[key], added...)
	}
	index.cves = regrouped
}

// liveAdvisoryPackages drops the facts of retracted records from one package's
// classification when another record still asserts the package (#9914). Grouping
// puts a withdrawn DLA and a live DEBIAN-CVE record in one group, and the group
// is not retracted because one record is live, so without this the withdrawn
// record's range could decide an exact verdict with no retraction shown.
//
// A record is retracted when every cve observation of its AdvisoryID retracted:
// the same rule retractionFromObservations applies to a group, so one source's
// rejection does not hide what another still asserts. A package with no cve
// observation counts as live, as it does there. When no package's record is live
// nothing is dropped, so a wholly retracted group keeps its verdict and its
// retraction (#9695).
//
// The dropped facts decide nothing but stay on the finding: the returned group
// carries their ids to EvidenceFactIDs, and the record's cve observation stays
// in the group, so AdvisorySources still shows its status. The input slice is
// never modified: the index shares it across installed rows. A group with no
// retracted observation returns without allocating.
func liveAdvisoryPackages(
	cves supplyChainCVEGroup,
	pkgs []supplychainmodel.AffectedPackage,
) (supplyChainCVEGroup, []supplychainmodel.AffectedPackage) {
	if !slices.ContainsFunc(cves.observations, impactCVERetracted) {
		return cves, pkgs
	}
	retracted := make(map[string]bool, len(cves.observations))
	for _, cve := range cves.observations {
		isRetracted := impactCVERetracted(cve)
		if prior, seen := retracted[cve.AdvisoryID]; seen {
			isRetracted = isRetracted && prior
		}
		retracted[cve.AdvisoryID] = isRetracted
	}
	live := make([]supplychainmodel.AffectedPackage, 0, len(pkgs))
	var retired []string
	for _, pkg := range pkgs {
		if retracted[pkg.AdvisoryID] {
			retired = append(retired, pkg.FactID)
			continue
		}
		live = append(live, pkg)
	}
	if len(live) == 0 || len(retired) == 0 {
		return cves, pkgs
	}
	cves.retiredAffectedFactIDs = retired
	return cves, live
}

// impactCVERetracted reports whether one cve observation retracted its advisory.
func impactCVERetracted(cve supplychainmodel.ImpactCVE) bool {
	return advisoryRetractionCode(cve.Status, cve.WithdrawnAt) != ""
}

// upstreamCVEKey returns the canonical CVE id an upstream entry names: the
// entry trimmed, with a case-insensitive CVE- prefix and something after it.
// Other entries (DEBIAN-CVE-*, UBUNTU-CVE-*, DSA-*) are not group keys, or a
// DSA would also form a group under its DEBIAN-CVE twin.
func upstreamCVEKey(entry string) (string, bool) {
	entry = strings.TrimSpace(entry)
	if len(entry) <= len("CVE-") || !strings.EqualFold(entry[:len("CVE-")], "CVE-") {
		return "", false
	}
	if entry[:len("CVE-")] == "CVE-" {
		return entry, true
	}
	return strings.ToUpper(entry), true
}

// hasUpstreamCVEKey reports whether the record names a CVE in its upstream
// list other than the group key it already has. It does not allocate.
func hasUpstreamCVEKey(cve supplychainmodel.ImpactCVE) bool {
	for _, entry := range cve.Upstream {
		if key, ok := upstreamCVEKey(entry); ok && key != cve.CVEID {
			return true
		}
	}
	return false
}

// upstreamGroupKeys returns the sorted, de-duplicated group keys of a record
// that names upstream CVEs: its own key when that is a CVE id, and every CVE
// id in its upstream list.
func upstreamGroupKeys(cve supplychainmodel.ImpactCVE) []string {
	keys := make([]string, 0, len(cve.Upstream)+1)
	if _, ok := upstreamCVEKey(cve.CVEID); ok {
		keys = append(keys, cve.CVEID)
	}
	for _, entry := range cve.Upstream {
		if key, ok := upstreamCVEKey(entry); ok && !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// osAdvisoryUpstreamSource returns the closed source label for a record the
// pass examines, or "" for one it does not. A record is examined when its id
// carries an OS vendor prefix or it carries an upstream list. The label comes
// from the id prefix only so it stays inside the counter's bounded set.
func osAdvisoryUpstreamSource(cve supplychainmodel.ImpactCVE) string {
	id := cve.AdvisoryID
	if id == "" {
		id = cve.CVEID
	}
	switch {
	case hasFoldPrefix(id, "DEBIAN-"), hasFoldPrefix(id, "DSA-"), hasFoldPrefix(id, "DLA-"), hasFoldPrefix(id, "DTSA-"):
		return "debian"
	case hasFoldPrefix(id, "UBUNTU-"), hasFoldPrefix(id, "USN-"), hasFoldPrefix(id, "LSN-"):
		return "ubuntu"
	case hasFoldPrefix(id, "ALPINE-"):
		return "alpine"
	case hasFoldPrefix(id, "ALSA-"), hasFoldPrefix(id, "ALBA-"), hasFoldPrefix(id, "ALEA-"):
		return "alma"
	case hasFoldPrefix(id, "RLSA-"), hasFoldPrefix(id, "RLBA-"), hasFoldPrefix(id, "RLEA-"):
		return "rocky"
	}
	if len(cve.Upstream) > 0 {
		return "other"
	}
	return ""
}

// hasFoldPrefix reports whether s starts with prefix, ignoring ASCII case,
// without allocating.
func hasFoldPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

// emitOSAdvisoryUpstreamGroups records one counter point per non-zero
// (source, result) cell.
func (h SupplyChainImpactHandler) emitOSAdvisoryUpstreamGroups(ctx context.Context, cells map[osUpstreamGroupKey]int) {
	if h.Instruments == nil || h.Instruments.SupplyChainImpactOSAdvisoryUpstreamGroups == nil {
		return
	}
	for key, count := range cells {
		if count == 0 {
			continue
		}
		h.Instruments.SupplyChainImpactOSAdvisoryUpstreamGroups.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrSource(key.source),
			telemetry.AttrResult(key.result),
		))
	}
}
