// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// TestNeo4jEntityIDAnchorLabelsMatchSchema pins both anchor label lists to the
// Neo4j schema DDL, so a label that gains a uid or id uniqueness constraint
// cannot silently fall out of the entity-id anchor (issue #7057).
func TestNeo4jEntityIDAnchorLabelsMatchSchema(t *testing.T) {
	t.Parallel()

	stmts, err := graph.SchemaStatementsForBackend(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend() error = %v", err)
	}
	uidRe := regexp.MustCompile(`FOR \((\w+):(\w+)\) REQUIRE (\w+)\.uid IS UNIQUE`)
	idRe := regexp.MustCompile(`FOR \((\w+):(\w+)\) REQUIRE (\w+)\.id IS UNIQUE`)
	var uidLabels, idLabels []string
	for _, stmt := range stmts {
		if m := uidRe.FindStringSubmatch(stmt); m != nil && m[1] == m[3] {
			uidLabels = append(uidLabels, m[2])
		}
		if m := idRe.FindStringSubmatch(stmt); m != nil && m[1] == m[3] {
			idLabels = append(idLabels, m[2])
		}
	}
	slices.Sort(uidLabels)
	slices.Sort(idLabels)
	if len(uidLabels) == 0 || len(idLabels) == 0 {
		t.Fatalf("parsed %d uid and %d id constraint labels; the DDL shape changed", len(uidLabels), len(idLabels))
	}
	if got := slices.Sorted(slices.Values(neo4jEntityUIDAnchorLabels)); !slices.Equal(got, uidLabels) {
		t.Errorf("neo4jEntityUIDAnchorLabels = %v\nschema uid-constrained labels = %v", got, uidLabels)
	}
	if got := slices.Sorted(slices.Values(neo4jEntityIDAnchorLabels)); !slices.Equal(got, idLabels) {
		t.Errorf("neo4jEntityIDAnchorLabels = %v\nschema id-constrained labels = %v", got, idLabels)
	}
	for _, label := range idLabels {
		if slices.Contains(uidLabels, label) {
			t.Errorf("label %q is in both lists; its nodes would be probed twice", label)
		}
	}
}

// TestNeo4jEntityIDAnchorShape pins the rendered clause: an uncorrelated
// CALL subquery with an inline {uid:} branch and an inline {id:} branch, and
// no WHERE-side id-OR-uid predicate.
func TestNeo4jEntityIDAnchorShape(t *testing.T) {
	t.Parallel()

	got := Neo4jEntityIDAnchor("e", "$entity_id")
	for _, want := range []string{
		"CALL {\n",
		"MATCH (e:AnalyticsModel|",
		"|Variable {uid: $entity_id})",
		"MATCH (e:CloudAction|Endpoint|EvidenceArtifact|Platform|Repository|Workload|WorkloadInstance {id: $entity_id})",
		"UNION",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Neo4jEntityIDAnchor() missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "WHERE") || strings.Contains(got, " OR ") {
		t.Errorf("Neo4jEntityIDAnchor() must anchor inline, not filter:\n%s", got)
	}
}
