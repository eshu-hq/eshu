// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

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
func resolveDeployableUnitMaterializedEdges(odu Odu, expectedEdgesPath string) (bool, string) {
	expected, err := LoadExpectedEdges(expectedEdgesPath)
	if err != nil {
		return false, err.Error()
	}
	registry, err := MaterializedEdgeDomainEdgeTypes("deployable_unit_edges")
	if err != nil {
		return false, err.Error()
	}
	if missing := missingDeployableUnitExpectedTypes(expected, registry); len(missing) > 0 {
		return false, fmt.Sprintf("odù %q: expected-edge set does not cover all registry types, missing: %v", odu.Name, missing)
	}

	evidence := DiscoveredEvidence(odu)
	_, resolved := relationships.Resolve(evidence, nil, relationships.DefaultConfidenceThreshold)
	if len(resolved) == 0 {
		return false, fmt.Sprintf("odù %q: the production evidence/resolution seams (DiscoveredEvidence -> relationships.Resolve) found zero resolved relationships in its own facts; deployable_unit_edges cannot admit any candidate without at least one RelDeploysFrom relationship", odu.Name)
	}

	intent, err := deployableUnitFamilyIntentFromOdu(odu)
	if err != nil {
		return false, err.Error()
	}
	rows, evaluation, err := reducer.ExtractDeployableUnitCorrelationRows(intent, odu.Facts, resolved)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractDeployableUnitCorrelationRows: %v", odu.Name, err)
	}
	if len(evaluation.Results) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractDeployableUnitCorrelationRows evaluated zero deployable-unit candidates from the odù's own workload signals", odu.Name)
	}

	admitted := reducer.AdmittedDeployableUnitRows(rows)
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
	return true, fmt.Sprintf(
		"odù %q: DiscoveredEvidence -> relationships.Resolve -> ExtractDeployableUnitCorrelationRows reproduces the expected %d-edge set exactly across all %d registry types",
		odu.Name, len(expected), len(registry),
	)
}

// deployableUnitFamilyIntentFromOdu derives the reducer.Intent the pure
// extraction seam needs from the Odù's own facts, never a hard-coded
// literal: every repository fact's name and graph_id become EntityKeys, so
// filterDeployableUnitCandidates admits every workload candidate the Odù's
// own repository facts describe, and ScopeID/GenerationID/SourceSystem come
// from the facts themselves so the guard stays correct if the catalog's
// scope or generation identifiers ever change.
func deployableUnitFamilyIntentFromOdu(odu Odu) (reducer.Intent, error) {
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
