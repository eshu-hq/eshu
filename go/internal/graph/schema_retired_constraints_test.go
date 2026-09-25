// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "testing"

// TestIsNeo4jRetiredConstraintMatchesKeywordCase is the #7100 review
// follow-up: Cypher keywords are case-insensitive, so a differently-cased
// CREATE CONSTRAINT statement naming a retired constraint must still be
// recognized as retired, matching the sibling adoption parser in
// cmd/bootstrap-data-plane, which uses strings.EqualFold for its keywords.
func TestIsNeo4jRetiredConstraintMatchesKeywordCase(t *testing.T) {
	t.Parallel()

	for _, statement := range []string{
		"CREATE CONSTRAINT tf_module_unique IF NOT EXISTS FOR (m:TerraformModule) REQUIRE (m.name, m.path) IS UNIQUE",
		"create constraint tf_module_unique if not exists for (m:TerraformModule) require (m.name, m.path) is unique",
		"Create Constraint helm_values_unique IF NOT EXISTS FOR (hv:HelmValues) REQUIRE hv.path IS UNIQUE",
	} {
		if !isNeo4jRetiredConstraint(statement) {
			t.Errorf("isNeo4jRetiredConstraint(%q) = false, want true", statement)
		}
	}
	if isNeo4jRetiredConstraint("CREATE CONSTRAINT some_other_unique IF NOT EXISTS FOR (n:Foo) REQUIRE n.uid IS UNIQUE") {
		t.Errorf("isNeo4jRetiredConstraint(other constraint) = true, want false")
	}
}
