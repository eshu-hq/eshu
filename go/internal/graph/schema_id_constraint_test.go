// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"regexp"
	"slices"
	"testing"
)

// TestHasIDUniquenessConstraintFollowsTheSchemaDDL checks the derived set
// against labels whose constraint kind is known from schemaConstraints:
// id-keyed, uid-keyed only, and path-keyed.
func TestHasIDUniquenessConstraintFollowsTheSchemaDDL(t *testing.T) {
	t.Parallel()

	for _, label := range []string{"Repository", "Workload", "WorkloadInstance", "Endpoint"} {
		if !HasIDUniquenessConstraint(label) {
			t.Errorf("HasIDUniquenessConstraint(%q) = false, want true (schema has an id uniqueness constraint)", label)
		}
	}
	for _, label := range []string{"Function", "File", "Directory", "Environment", "NoSuchLabel", "repository"} {
		if HasIDUniquenessConstraint(label) {
			t.Errorf("HasIDUniquenessConstraint(%q) = true, want false", label)
		}
	}
}

// TestIDUniquenessConstrainedLabelsIsTheSchemaDDLSet pins the enumeration the
// Neo4j entity-context anchor seeks on id (#7212): exactly the labels the
// Neo4j DDL gives a single-property id uniqueness constraint, sorted, once
// each, and a fresh copy on every call.
func TestIDUniquenessConstrainedLabelsIsTheSchemaDDLSet(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(neo4j) error = %v", err)
	}
	idRe := regexp.MustCompile(`FOR \((\w+):(\w+)\) REQUIRE (\w+)\.id IS UNIQUE`)
	var want []string
	for _, stmt := range stmts {
		if m := idRe.FindStringSubmatch(stmt); m != nil && m[1] == m[3] && !slices.Contains(want, m[2]) {
			want = append(want, m[2])
		}
	}
	slices.Sort(want)
	got := IDUniquenessConstrainedLabels()
	if len(want) == 0 || !slices.Equal(got, want) {
		t.Fatalf("IDUniquenessConstrainedLabels() = %v, want the DDL set %v", got, want)
	}
	got[0] = "Mutated"
	if IDUniquenessConstrainedLabels()[0] == "Mutated" {
		t.Error("IDUniquenessConstrainedLabels() shares its backing array with callers")
	}
}
