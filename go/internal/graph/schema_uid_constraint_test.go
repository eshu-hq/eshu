// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"fmt"
	"regexp"
	"slices"
	"testing"
)

// TestHasUIDUniquenessConstraintMatchesSchemaDDL pins the reader-facing
// predicate to the DDL each backend actually applies: a label reports true
// exactly when both the Neo4j and the NornicDB statement lists create its
// <label>_uid_unique constraint. Query handlers anchor a by-id read on uid
// only for these labels (issue #7089), so a true answer for a label without
// the constraint would plan a label scan, and a false answer would leave a
// seekable label on the scan path.
func TestHasUIDUniquenessConstraintMatchesSchemaDDL(t *testing.T) {
	t.Parallel()

	for _, backend := range []SchemaBackend{SchemaBackendNeo4j, SchemaBackendNornicDB} {
		stmts, err := SchemaStatementsForBackend(backend)
		if err != nil {
			t.Fatalf("SchemaStatementsForBackend(%q) error = %v", backend, err)
		}
		for _, label := range uidConstraintLabels {
			want := fmt.Sprintf(
				"CREATE CONSTRAINT %s_uid_unique IF NOT EXISTS FOR (n:%s) REQUIRE n.uid IS UNIQUE",
				labelToSnake(label), label,
			)
			if !slices.Contains(stmts, want) {
				t.Errorf("%s schema does not create the uid constraint for %q", backend, label)
			}
			if !HasUIDUniquenessConstraint(label) {
				t.Errorf("HasUIDUniquenessConstraint(%q) = false, want true", label)
			}
		}
	}
	for _, label := range []string{"Function", "Class", "File", "Module", "TypeAnnotation"} {
		if !HasUIDUniquenessConstraint(label) {
			t.Errorf("HasUIDUniquenessConstraint(%q) = false, want true", label)
		}
	}
	// Repository, Workload and WorkloadInstance are keyed by id; Directory by
	// path; Rationale carries only a Neo4j uid RANGE index, not a constraint.
	for _, label := range []string{"Repository", "Workload", "WorkloadInstance", "Directory", "Rationale", "function", ""} {
		if HasUIDUniquenessConstraint(label) {
			t.Errorf("HasUIDUniquenessConstraint(%q) = true, want false", label)
		}
	}
}

// TestUIDUniquenessConstrainedLabelsIsTheSchemaDDLSet pins the enumeration the
// Neo4j entity-context anchor seeks on uid (#7212): exactly the labels each
// backend's DDL gives a single-property uid uniqueness constraint, sorted,
// once each, and a fresh copy on every call.
func TestUIDUniquenessConstrainedLabelsIsTheSchemaDDLSet(t *testing.T) {
	t.Parallel()

	got := UIDUniquenessConstrainedLabels()
	if !slices.IsSorted(got) || len(slices.Compact(slices.Clone(got))) != len(got) {
		t.Fatalf("UIDUniquenessConstrainedLabels() = %v, want sorted and without duplicates", got)
	}
	uidRe := regexp.MustCompile(`FOR \((\w+):(\w+)\) REQUIRE (\w+)\.uid IS UNIQUE`)
	for _, backend := range []SchemaBackend{SchemaBackendNeo4j, SchemaBackendNornicDB} {
		stmts, err := SchemaStatementsForBackend(backend)
		if err != nil {
			t.Fatalf("SchemaStatementsForBackend(%q) error = %v", backend, err)
		}
		var want []string
		for _, stmt := range stmts {
			if m := uidRe.FindStringSubmatch(stmt); m != nil && m[1] == m[3] && !slices.Contains(want, m[2]) {
				want = append(want, m[2])
			}
		}
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: UIDUniquenessConstrainedLabels() = %v (%d), want the DDL set %v (%d)", backend, got, len(got), want, len(want))
		}
	}
	got[0] = "Mutated"
	if UIDUniquenessConstrainedLabels()[0] == "Mutated" {
		t.Error("UIDUniquenessConstrainedLabels() shares its backing array with callers")
	}
}
