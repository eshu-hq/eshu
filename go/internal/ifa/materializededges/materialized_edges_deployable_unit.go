// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// deployableUnitEdgesFamily is the materialized-edge family key this guard
// asserts, mirroring codeownersOwnershipFamily's role for its own family: it
// names the fixture's identity contract as well as its edge-type registry,
// so MaterializedEdgeOduResolver.Resolve's dispatch and this file's own
// LoadExpectedEdges/MaterializedEdgeDomainEdgeTypes calls can never be
// pointed at different families by a typo in one of them.
const deployableUnitEdgesFamily = "deployable_unit_edges"

// deployableUnitFamilyOduName, deployableUnitFamilyGenerationID,
// deployableUnitFamilyAppRepoID, deployableUnitFamilyAppRepoName,
// deployableUnitFamilyRejectedRepoID, and
// deployableUnitFamilyAdmittedNoDeployRepoID duplicate the deployable-unit
// family's catalog identifiers from ifa's deployable_unit_family_catalog.go:
// this package cannot reach those unexported constants across the package
// boundary, and deployable_unit_family_catalog.go / catalog_seed.go /
// deployable_unit_family_odu_test.go still need their own copies to stay in
// ifa, so this is a copy, not a move.
const (
	deployableUnitFamilyOduName                = "odu:ifa-deployable-unit-family"
	deployableUnitFamilyGenerationID           = "gen-ifa-deployable-unit-family-1"
	deployableUnitFamilyAppRepoID              = "repo-ifa-deployable-unit-app"
	deployableUnitFamilyAppRepoName            = "deployable-unit-family"
	deployableUnitFamilyRejectedRepoID         = "repo-ifa-deployable-unit-rejected"
	deployableUnitFamilyAdmittedNoDeployRepoID = "repo-ifa-deployable-unit-jenkins"
)

// deployableUnitGuardClock is the fixed, deterministic wall-clock reading
// this guard hands to reducer.ExtractDeployableUnitCorrelationRows for every
// row's CreatedAt. go/internal/ifa/AGENTS.md forbids wall-clock time inside
// Ifá derivation; the extraction seam takes an injectable clock exactly so
// this guard never calls real time.Now(). The value matches the committed
// cassette's observed_at so a reader correlating the two finds the same
// instant, though CreatedAt itself is never part of the edge-identity
// comparison this guard performs.
var deployableUnitGuardClock = time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)

// deployableUnitExpectedEdgesPath is where the family's hand-derived
// expected-edge-set fixture lives. Under go/internal/ifa/testdata/, not
// testdata/cassettes/, for the same reason the SQL, documentation, code-call,
// and rationale families' fixtures do: the offline cassette validator globs
// every testdata/cassettes/*/*.json as a replay cassette, and this file is a
// gate ASSERTION, not a cassette.
const deployableUnitExpectedEdgesRelPath = "go/internal/ifa/testdata/deployableunit/ifa-deployable-unit-family-expected-edges.json"

// deployableUnitFamilyExpectedEdgesPath joins repoRoot onto the expected-edge
// fixture.
func deployableUnitFamilyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, deployableUnitExpectedEdgesRelPath)
}

// resolveDeployableUnitMaterializedEdges is deployable_unit_edges' named
// vacuity guard, mirroring resolveSQLRelationshipMaterializedEdges /
// resolveCodeCallMaterializedEdges's role for their families.
//
// Unlike those two, this family's edges cannot be derived from an Odù's own
// facts through one pure fact-only extractor: a deployable-unit candidate's
// deployment_repo_id only exists once a RelDeploysFrom resolved relationship
// is applied (reducer.ExtractDeployableUnitCorrelationRows's doc comment),
// and production computes that relationship from raw evidence, not from a
// hand-built value. So this guard runs the same two seams the real
// cross-repo resolution phase runs -- DiscoveredEvidence (ifa's own
// DiscoverEvidence wrapper, #4394) then relationships.Resolve -- over the
// Odù's own facts before calling the pure row-extraction seam. A guard that
// hand-authored a relationships.ResolvedRelationship instead would certify a
// deployable-unit edge no live graph backend could actually reach, exactly
// the false-green class the #5351 live-proof finding warns about for
// endpoint identity; running the real resolver closes that gap for the
// resolved-relationship input too.
func resolveDeployableUnitMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, err := LoadExpectedEdges(expectedEdgesPath, deployableUnitEdgesFamily)
	if err != nil {
		return false, err.Error()
	}
	registry, err := MaterializedEdgeDomainEdgeTypes(deployableUnitEdgesFamily)
	if err != nil {
		return false, err.Error()
	}
	if missing := missingDeployableUnitExpectedTypes(expected, registry); len(missing) > 0 {
		return false, fmt.Sprintf("odù %q: expected-edge set does not cover all registry types, missing: %v", odu.Name, missing)
	}

	evidence := ifa.DiscoveredEvidence(odu)
	_, resolved := relationships.Resolve(evidence, nil, relationships.DefaultConfidenceThreshold)
	if len(resolved) == 0 {
		return false, fmt.Sprintf("odù %q: the production evidence/resolution seams (DiscoveredEvidence -> relationships.Resolve) found zero resolved relationships in its own facts; deployable_unit_edges cannot admit any candidate without at least one RelDeploysFrom relationship", odu.Name)
	}

	intent, err := deployableUnitFamilyIntentFromOdu(odu)
	if err != nil {
		return false, err.Error()
	}
	// ExtractDeployableUnitCorrelationRows takes already-extracted candidates,
	// not raw fact envelopes, matching production's Handle (#5993 review): the
	// extraction step (ExtractWorkloadCandidates) is itself pure, so doing it
	// here rather than inside the seam does not weaken this guard's claim to
	// run the same code path production runs.
	candidates, _ := reducer.ExtractWorkloadCandidates(odu.Facts)
	rows, evaluation, err := reducer.ExtractDeployableUnitCorrelationRows(intent, candidates, resolved, func() time.Time { return deployableUnitGuardClock })
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractDeployableUnitCorrelationRows: %v", odu.Name, err)
	}
	if len(evaluation.Results) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractDeployableUnitCorrelationRows evaluated zero deployable-unit candidates from the odù's own workload signals", odu.Name)
	}

	admitted := reducer.AdmittedDeployableUnitRows(rows)
	if detail := deployableUnitAssertDropReasons(odu.Name, rows, admitted); detail != "" {
		return false, detail
	}

	actual := make([]ExpectedEdge, 0, len(admitted))
	for _, row := range admitted {
		actual = append(actual, ExpectedEdge{
			RelationshipType: anyToStringValue(row.Payload["relationship_type"]),
			SourceEntityID:   anyToStringValue(row.Payload["repo_id"]),
			TargetEntityID:   anyToStringValue(row.Payload["deployment_repo_id"]),
		})
	}
	if mismatch := compareDeployableUnitExpectedEdges(odu.Name, expected, actual); mismatch != "" {
		return false, mismatch
	}

	// The MERGE key is bare (source_repo.id + deployment_repo.id + type only,
	// canonical_deployable_unit_edges.go), so the ExpectedEdge triple above
	// cannot distinguish a correct correlation from a wrong one for the SAME
	// repo pair: two different correlation reasons collapse onto one edge,
	// last-writer-wins on all 14 SET properties. Assert the ones that carry
	// correlation truth so a regression that admits the right repo pair for
	// the WRONG reason (wrong rule pack, wrong admission state, stale
	// generation) still fails this guard.
	if detail := deployableUnitAssertAdmittedEdgeProperties(odu.Name, admitted); detail != "" {
		return false, detail
	}

	return true, fmt.Sprintf(
		"odù %q: DiscoveredEvidence -> relationships.Resolve -> ExtractDeployableUnitCorrelationRows reproduces the expected %d-edge set exactly across all %d registry types, proves both AdmittedDeployableUnitRows drop reasons, and matches the admitted edge's non-identity properties",
		odu.Name, len(expected), len(registry),
	)
}

// deployableUnitAssertDropReasons proves AdmittedDeployableUnitRows' two
// independent drop conditions actually fire for this Odù, not merely that
// the admitted set happens to match the expected edges. A fixture carrying
// only rows that end up admitted cannot prove the filter exists: removing it
// entirely would still pass an exact-set comparison over an Odù with no
// candidate that SHOULD be dropped.
func deployableUnitAssertDropReasons(oduName string, rows, admitted []reducer.SharedProjectionIntentRow) string {
	admittedRepoIDs := make(map[string]struct{}, len(admitted))
	for _, row := range admitted {
		admittedRepoIDs[row.RepositoryID] = struct{}{}
	}

	var rejectedRow, noDeployRow *reducer.SharedProjectionIntentRow
	for i := range rows {
		switch rows[i].RepositoryID {
		case deployableUnitFamilyRejectedRepoID:
			rejectedRow = &rows[i]
		case deployableUnitFamilyAdmittedNoDeployRepoID:
			noDeployRow = &rows[i]
		}
	}

	if rejectedRow == nil {
		return fmt.Sprintf("odù %q: no row for %q; the rejected-candidate drop reason cannot be proven", oduName, deployableUnitFamilyRejectedRepoID)
	}
	if anyToStringValue(rejectedRow.Payload["admission_state"]) == "admitted" {
		return fmt.Sprintf("odù %q: %q admitted unexpectedly; the Dockerfile-only fixture no longer proves the rejection drop reason", oduName, deployableUnitFamilyRejectedRepoID)
	}
	if _, ok := admittedRepoIDs[deployableUnitFamilyRejectedRepoID]; ok {
		return fmt.Sprintf("odù %q: rejected candidate %q leaked into the admitted set", oduName, deployableUnitFamilyRejectedRepoID)
	}

	if noDeployRow == nil {
		return fmt.Sprintf("odù %q: no row for %q; the blank-deployment_repo_id drop reason cannot be proven", oduName, deployableUnitFamilyAdmittedNoDeployRepoID)
	}
	if anyToStringValue(noDeployRow.Payload["admission_state"]) != "admitted" {
		return fmt.Sprintf("odù %q: %q no longer reaches admission via structural evidence alone; the fixture no longer proves the blank-deployment_repo_id drop reason", oduName, deployableUnitFamilyAdmittedNoDeployRepoID)
	}
	if strings.TrimSpace(anyToStringValue(noDeployRow.Payload["deployment_repo_id"])) != "" {
		return fmt.Sprintf("odù %q: %q unexpectedly carries a deployment_repo_id; the fixture no longer isolates the blank-deployment_repo_id drop reason", oduName, deployableUnitFamilyAdmittedNoDeployRepoID)
	}
	if _, ok := admittedRepoIDs[deployableUnitFamilyAdmittedNoDeployRepoID]; ok {
		return fmt.Sprintf("odù %q: admitted-but-no-deploy candidate %q leaked into the admitted set", oduName, deployableUnitFamilyAdmittedNoDeployRepoID)
	}

	return ""
}

// deployableUnitAssertAdmittedEdgeProperties checks the admitted edge's
// non-identity properties against the values this Odù's evidence must
// produce. See resolveDeployableUnitMaterializedEdges's call site for why:
// the bare MERGE key means the ExpectedEdge triple alone cannot catch a
// correct repo pair admitted for the wrong reason.
func deployableUnitAssertAdmittedEdgeProperties(oduName string, admitted []reducer.SharedProjectionIntentRow) string {
	if len(admitted) != 1 {
		return fmt.Sprintf("odù %q: expected exactly 1 admitted row to assert non-identity properties against, got %d", oduName, len(admitted))
	}
	row := admitted[0]
	want := map[string]string{
		"admission_state":     "admitted",
		"evidence_type":       "deployable_unit_correlation",
		"resolution_source":   "reducer/deployable-unit-correlation",
		"generation_id":       deployableUnitFamilyGenerationID,
		"deployable_unit_key": deployableUnitFamilyAppRepoName,
		"correlation_key":     deployableUnitFamilyAppRepoID + ":" + deployableUnitFamilyAppRepoName,
		"rule_pack":           "argocd",
	}
	var mismatches []string
	for key, wantValue := range want {
		if got := anyToStringValue(row.Payload[key]); got != wantValue {
			mismatches = append(mismatches, fmt.Sprintf("%s=%q (want %q)", key, got, wantValue))
		}
	}
	if len(mismatches) == 0 {
		return ""
	}
	sort.Strings(mismatches)
	return fmt.Sprintf("odù %q: admitted edge non-identity properties do not match: %s", oduName, strings.Join(mismatches, ", "))
}

// deployableUnitFamilyIntentFromOdu derives the reducer.Intent the pure
// extraction seam needs from the Odù's own facts, never a hard-coded
// literal: every repository fact's name and graph_id become EntityKeys, so
// filterDeployableUnitCandidates admits every workload candidate the Odù's
// own repository facts describe, and ScopeID/GenerationID/SourceSystem come
// from the facts themselves so the guard stays correct if the catalog's
// scope or generation identifiers ever change.
func deployableUnitFamilyIntentFromOdu(odu ifa.Odu) (reducer.Intent, error) {
	if len(odu.Facts) == 0 {
		return reducer.Intent{}, fmt.Errorf("ifa: odù %q carries no facts", odu.Name)
	}
	var entityKeys []string
	for _, envelope := range odu.Facts {
		if envelope.FactKind != deployableUnitRepositoryFactKind {
			continue
		}
		for _, key := range []string{"name", "graph_id"} {
			if value, ok := envelope.Payload[key].(string); ok && strings.TrimSpace(value) != "" {
				entityKeys = append(entityKeys, value)
			}
		}
	}
	if len(entityKeys) == 0 {
		return reducer.Intent{}, fmt.Errorf("ifa: odù %q carries no repository facts to derive entity keys from", odu.Name)
	}
	first := odu.Facts[0]
	return reducer.Intent{
		IntentID:     "ifa:" + odu.Name,
		ScopeID:      first.ScopeID,
		GenerationID: first.GenerationID,
		SourceSystem: first.CollectorKind,
		Domain:       reducer.DomainDeployableUnitCorrelation,
		Cause:        "ifa deployable_unit_edges materialized-edge vacuity guard",
		EntityKeys:   entityKeys,
	}, nil
}

// deployableUnitRepositoryFactKind is the "repository" fact kind literal, named
// locally so this file does not need to import sdk/go/factschema solely for
// one string constant already equal to it.
const deployableUnitRepositoryFactKind = "repository"

func missingDeployableUnitExpectedTypes(expected []ExpectedEdge, registry map[string]struct{}) []string {
	present := make(map[string]struct{}, len(expected))
	for _, edge := range expected {
		present[edge.RelationshipType] = struct{}{}
	}
	var missing []string
	for edgeType := range registry {
		if _, ok := present[edgeType]; !ok {
			missing = append(missing, edgeType)
		}
	}
	sort.Strings(missing)
	return missing
}

func compareDeployableUnitExpectedEdges(oduName string, expected, actual []ExpectedEdge) string {
	want := make(map[string]int, len(expected))
	got := make(map[string]int, len(actual))
	for _, edge := range expected {
		want[edge.Key()]++
	}
	for _, edge := range actual {
		got[edge.Key()]++
	}
	var missing, extra []string
	for key, count := range want {
		if got[key] < count {
			missing = append(missing, key)
		}
	}
	for key, count := range got {
		if count > want[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) == 0 && len(extra) == 0 {
		return ""
	}
	return fmt.Sprintf("odù %q: deployable-unit edge set does not match %d hand-derived edge(s); MISSING: %s; EXTRA: %s", oduName, len(expected), strings.Join(missing, ", "), strings.Join(extra, ", "))
}
