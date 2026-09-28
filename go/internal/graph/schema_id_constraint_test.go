// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "testing"

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
