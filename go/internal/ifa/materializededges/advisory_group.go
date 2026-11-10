// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu-sdk/factschema"
	reducerderivedv1 "github.com/eshu-hq/eshu-sdk/factschema/reducerderived/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// advisoryGroupFamily is the materialized-edge family key this guard
// asserts (#10466), matching the key the advisory_group entry registers
// in materialized.singleTypeMaterializedEdgeFamilies.
const advisoryGroupFamily = "advisory_group"

// advisoryGroupRelationshipType is the single relationship type the
// AdvisoryGroupUpsertCypher template MERGEs, a static token in the
// template. Duplicated as a literal and failing closed for the same
// reason as runtimeDeclarationRelationshipType: the fixture is compared
// against the registry, so a stale literal reds instead of asserting
// less.
const advisoryGroupRelationshipType = "AFFECTED_BY"

// advisoryGroupExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const advisoryGroupExpectedEdgesRelPath = "go/internal/ifa/testdata/advisorygroup/ifa-advisory-group-family-expected-edges.json"

// advisoryGroupOperatorScopeID mirrors
// postgres.supplyChainImpactOperatorScopeID: the operator suppression
// scope overlays state and never sources a winner.
const advisoryGroupOperatorScopeID = "operator:vulnerability_suppressions"

// advisoryGroupExpectedEdgesPath joins repoRoot onto the fixture.
func advisoryGroupExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, advisoryGroupExpectedEdgesRelPath)
}

// AdvisoryGroupID renders the AdvisoryGroup node identity for one winner
// row: the CVE id when the row names one, else the advisory id — the
// exact CASE the winners reader groups on
// (advisoryGroupWinnersQuery) and the id the writer MERGEs the
// AdvisoryGroup node by.
//
// It is the ONE shared derivation for that identity on the proof side:
// the vacuity guard below renders each expected edge's target through
// it, and the live assert-edges scan reads the same value off the
// graph node's id property, so a grouping-rule change reds in one
// place instead of drifting between the fixture and the gate. It fails
// closed: both ids blank renders "", which no writer could have MERGEd.
func AdvisoryGroupID(cveID, advisoryID string) string {
	if strings.TrimSpace(cveID) != "" {
		return cveID
	}
	return advisoryID
}

// advisoryGroupCandidate is one decoded finding fact staged for winner
// selection, carrying the stable key the SQL orders fact_id by.
type advisoryGroupCandidate struct {
	stableKey string
	finding   reducerderivedv1.SupplyChainImpactFinding
}

// advisoryGroupCanonicalKey renders the winner select's canonical_key
// CONCAT_WS('|', ...) over one finding payload
// (supplyChainImpactCanonicalKeyArgs): the dedup identity the
// maintainer rebuild picks one winner per.
func advisoryGroupCanonicalKey(f reducerderivedv1.SupplyChainImpactFinding) string {
	head := f.CVEID
	if head == "" {
		head = f.AdvisoryID
	}
	return strings.Join([]string{
		head, f.AdvisoryID, f.PackageID, f.PURL, f.ProductCriteria,
		f.MatchCriteriaID, f.ObservedVersion, f.RequestedRange,
		f.ImpactStatus, f.RepositoryID, f.SubjectDigest,
	}, "|")
}

// advisoryGroupWinnerLess orders two candidates for one canonical key
// the way source_winners' DISTINCT ON does: priority_score DESC, a
// payload finding_id first, then fact_id ASC — rendered here over the
// stable fact key, the identity a hermetic Odù carries. It reports
// whether a displaces b.
func advisoryGroupWinnerLess(a, b advisoryGroupCandidate) bool {
	if a.finding.PriorityScore != b.finding.PriorityScore {
		return a.finding.PriorityScore > b.finding.PriorityScore
	}
	aHas, bHas := a.finding.FindingID != "", b.finding.FindingID != ""
	if aHas != bHas {
		return aHas
	}
	return a.stableKey < b.stableKey
}

// advisoryGroupMembers extracts provenance.advisory_group_members from
// a decoded finding, accepting the []string the encoder emits and the
// []any a JSON round trip produces. A members value in any other shape
// is reported, never silently read as empty: an unreadable members key
// is exactly how #10475 hid.
func advisoryGroupMembers(f reducerderivedv1.SupplyChainImpactFinding) ([]string, error) {
	raw, ok := f.Provenance["advisory_group_members"]
	if !ok || raw == nil {
		return nil, nil
	}
	switch values := raw.(type) {
	case []string:
		return append([]string(nil), values...), nil
	case []any:
		members := make([]string, 0, len(values))
		for _, value := range values {
			member, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("advisory_group_members holds a non-string %T", value)
			}
			members = append(members, member)
		}
		return members, nil
	default:
		return nil, fmt.Errorf("advisory_group_members has unexpected type %T", raw)
	}
}

// advisoryGroupQualifying mirrors the winners reader's row filter
// (advisoryGroupWinnersQuery) over one winner: a qualifying impact
// status, an effective suppression state outside the hidden set, and a
// non-empty repository. Expiry overlay needs database time and the
// fixture carries no expiring suppression, so the state alone decides;
// the hidden set itself comes from the production source both sides
// derive from.
func advisoryGroupQualifying(f reducerderivedv1.SupplyChainImpactFinding) bool {
	switch supplychaincore.SupplyChainImpactStatus(f.ImpactStatus) {
	case supplychaincore.SupplyChainImpactAffectedExact,
		supplychaincore.SupplyChainImpactAffectedDerived,
		supplychaincore.SupplyChainImpactPossiblyAffected:
	default:
		return false
	}
	state := "active"
	if f.SuppressionState != nil && *f.SuppressionState != "" {
		state = *f.SuppressionState
	}
	for _, hidden := range supplychaincore.SupplyChainSuppressionHiddenStates() {
		if string(hidden) == state {
			return false
		}
	}
	return strings.TrimSpace(f.RepositoryID) != ""
}

// resolveAdvisoryGroupMaterializedEdges is advisory_group's named
// vacuity guard (#10466). It replays the family's production chain
// hermetically: decode the Odù's finding facts through the production
// factschema decoder, dedup them to canonical winners the way the
// maintainer rebuild's winner select does, filter to the winners
// reader's qualifying set, fold each group through the handler's pure
// BuildGroup seam, and require the resulting AFFECTED_BY set to equal
// the hand-derived fixture exactly.
//
// It fails closed beyond the sibling guards' factless check, per the
// #10466 arbiter conditions: an Odù with no finding facts, a qualifying
// winner carrying no provenance members, or a winner whose members
// never leave its own CVE/advisory ids (BuildGroup folds those in
// anyway, so the union assertion would be vacuous) cannot resolve
// covered. A quarantined-by-schema fact is fatal the same way: the
// decoder error means the fixture no longer validates against the
// finding contract.
func resolveAdvisoryGroupMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(expectedEdgesPath, advisoryGroupFamily, odu.Name)
	if problem != "" {
		return false, problem
	}
	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}

	candidates := make([]advisoryGroupCandidate, 0, len(odu.Facts))
	for _, env := range odu.Facts {
		if env.FactKind != facts.ReducerSupplyChainImpactFindingFactKind {
			continue
		}
		decoded, err := factschema.DecodeReducerSupplyChainImpactFinding(factschema.Envelope{
			SchemaVersion: env.SchemaVersion,
			Payload:       env.Payload,
		})
		if err != nil {
			return false, fmt.Sprintf("odù %q: finding fact %q no longer decodes against the reducer_supply_chain_impact_finding contract: %v", odu.Name, env.StableFactKey, err)
		}
		if env.IsTombstone || env.ScopeID == advisoryGroupOperatorScopeID || strings.HasPrefix(env.ScopeID, "image://derived/") {
			continue
		}
		candidates = append(candidates, advisoryGroupCandidate{stableKey: env.StableFactKey, finding: decoded})
	}
	if len(candidates) == 0 {
		return false, fmt.Sprintf("odù %q: carries no active reducer_supply_chain_impact_finding facts; a winners-fed family cannot be proven by an Odù with no findings to rebuild winners from", odu.Name)
	}

	winners := map[string]advisoryGroupCandidate{}
	for _, candidate := range candidates {
		key := advisoryGroupCanonicalKey(candidate.finding)
		if current, exists := winners[key]; !exists || advisoryGroupWinnerLess(candidate, current) {
			winners[key] = candidate
		}
	}

	rowsByGroup := map[string][]advisorygroup.WinnerRow{}
	for _, winner := range winners {
		f := winner.finding
		if !advisoryGroupQualifying(f) {
			continue
		}
		groupID := AdvisoryGroupID(f.CVEID, f.AdvisoryID)
		if groupID == "" {
			return false, fmt.Sprintf("odù %q: qualifying winner %q names neither a CVE nor an advisory id, so it belongs to no group the reader could match", odu.Name, winner.stableKey)
		}
		members, err := advisoryGroupMembers(f)
		if err != nil {
			return false, fmt.Sprintf("odù %q: winner %q: %v", odu.Name, winner.stableKey, err)
		}
		if len(members) == 0 {
			return false, fmt.Sprintf("odù %q: qualifying winner %q carries no provenance advisory_group_members; a members-less Odù cannot prove the member union (#10466)", odu.Name, winner.stableKey)
		}
		distinct := false
		for _, member := range members {
			if member != f.CVEID && member != f.AdvisoryID {
				distinct = true
				break
			}
		}
		if !distinct {
			return false, fmt.Sprintf("odù %q: qualifying winner %q carries no member distinct from its own CVE/advisory ids; BuildGroup folds those in regardless, so the union assertion would be vacuous (#10466)", odu.Name, winner.stableKey)
		}
		rowsByGroup[groupID] = append(rowsByGroup[groupID], advisorygroup.WinnerRow{
			RepositoryID: f.RepositoryID,
			PackageID:    f.PackageID,
			Ecosystem:    f.Ecosystem,
			PURL:         f.PURL,
			CVEID:        f.CVEID,
			AdvisoryID:   f.AdvisoryID,
			Members:      members,
		})
	}
	if len(rowsByGroup) == 0 {
		return false, fmt.Sprintf("odù %q: no finding survives winner selection and the qualifying filter; this fixture cannot prove anything", odu.Name)
	}

	groupIDs := make([]string, 0, len(rowsByGroup))
	for groupID := range rowsByGroup {
		groupIDs = append(groupIDs, groupID)
	}
	sort.Strings(groupIDs)

	actual := []ExpectedEdge{}
	unionDetail := []string{}
	for _, groupID := range groupIDs {
		group := advisorygroup.BuildGroup(groupID, rowsByGroup[groupID])
		if len(group.CVEIDs) == 0 || len(group.AdvisoryIDs) == 0 {
			return false, fmt.Sprintf("odù %q: group %q folds to cve_ids=%v advisory_ids=%v; the fixture must exercise the CVE/advisory member split on the node", odu.Name, groupID, group.CVEIDs, group.AdvisoryIDs)
		}
		unionDetail = append(unionDetail, fmt.Sprintf("%s cve_ids=%v advisory_ids=%v", groupID, group.CVEIDs, group.AdvisoryIDs))
		for _, edge := range group.Edges {
			actual = append(actual, ExpectedEdge{
				RelationshipType: advisoryGroupRelationshipType,
				SourceEntityID:   edge.RepositoryID,
				TargetEntityID:   groupID,
				Identity:         map[string]string{"package_id": edge.PackageID},
				Properties:       map[string]string{"ecosystem": edge.Ecosystem, "purl": edge.PURL},
			})
		}
	}
	if len(actual) == 0 {
		return false, fmt.Sprintf("odù %q: BuildGroup produced zero AFFECTED_BY edges; this fixture cannot prove anything", odu.Name)
	}
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, advisoryGroupFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: winner selection plus BuildGroup reproduces the expected %d-edge AFFECTED_BY set exactly across all %d registry type(s) and %d fact envelope(s); node union: %s; the repository-less, known-fixed and tombstoned findings added nothing",
		odu.Name, len(expected), len(registry), len(odu.Facts), strings.Join(unionDetail, "; "),
	)
}
