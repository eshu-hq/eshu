// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "regexp"

// idConstraintPattern matches a single-property id uniqueness constraint in
// schemaConstraints, e.g. `FOR (r:Repository) REQUIRE r.id IS UNIQUE`. RE2 has no
// backreferences, so the two node variables are compared in Go.
var idConstraintPattern = regexp.MustCompile(`FOR \((\w+):(\w+)\) REQUIRE (\w+)\.id IS UNIQUE`)

// idConstrainedLabelSet is every label schemaConstraints keys by an id
// uniqueness constraint, derived from the DDL itself so it cannot drift from
// what the schema applies.
var idConstrainedLabelSet = func() map[string]struct{} {
	set := make(map[string]struct{})
	for _, statement := range schemaConstraints {
		if m := idConstraintPattern.FindStringSubmatch(statement); m != nil && m[1] == m[3] {
			set[m[2]] = struct{}{}
		}
	}
	return set
}()

// HasIDUniquenessConstraint reports whether the graph schema gives label an id
// uniqueness constraint (Repository, Workload, WorkloadInstance, ...). On Neo4j
// a `MATCH (n:<label> {id: $x})` read of such a label plans as a unique index
// seek, while a label with no id constraint would be a label scan. Query
// handlers use it, with HasUIDUniquenessConstraint, to decide which labels an
// id anchor can seek (issue #7380). The label match is exact and case-sensitive.
func HasIDUniquenessConstraint(label string) bool {
	_, ok := idConstrainedLabelSet[label]
	return ok
}
