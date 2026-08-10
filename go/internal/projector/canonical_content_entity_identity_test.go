// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import "testing"

// TestContentEntityIDsAreDerivedNotTrusted is the #6020-review regression.
//
// Registering these labels gave them a `uid IS UNIQUE` constraint, and a
// constraint is only as good as the identity feeding it. canonicalGraphEntityID
// derives the ID from (repo, path, type, name, line) ONLY for labels listed in
// canonicalNamePathLineEntityLabels; for anything else it preserves whatever
// entity_id the producer supplied.
//
// So without that registration, two facts describing the SAME entity but
// carrying different supplied IDs -- a replay, a version-skewed producer, or a
// non-git producer -- both satisfy the uid constraint and materialize as two
// nodes. The constraint would have made the duplication look impossible while
// permitting exactly it.
//
// Asserted through extractEntities rather than through EntityTypeLabel, because
// the label map cannot see this: it answers "is this type recognised", not
// "which id will be written".
func TestContentEntityIDsAreDerivedNotTrusted(t *testing.T) {
	t.Parallel()

	types := []struct {
		entityType string
		label      string
	}{
		{"terraform_block", "TerraformBlock"},
		{"cloudformation_condition", "CloudFormationCondition"},
		{"cloudformation_import", "CloudFormationImport"},
		{"cloudformation_export", "CloudFormationExport"},
		{"pagerduty_declaration", "PagerDutyDeclaration"},
	}

	for _, tc := range types {
		// Same entity tuple, two different producer-supplied ids.
		first := canonicalGraphEntityID(
			tc.label, "repo-1", "infra/main.tf", tc.entityType, "probe", 7,
			"producer-supplied-id-A",
		)
		second := canonicalGraphEntityID(
			tc.label, "repo-1", "infra/main.tf", tc.entityType, "probe", 7,
			"producer-supplied-id-B",
		)

		if first != second {
			t.Errorf("%s: two facts for the same entity produced different uids (%q vs %q); "+
				"the uid constraint cannot stop the duplicate node, it only makes the "+
				"duplication look impossible", tc.label, first, second)
			continue
		}
		if first == "producer-supplied-id-A" {
			t.Errorf("%s: uid was taken from the producer's entity_id instead of derived "+
				"from (repo, path, type, name, line)", tc.label)
		}
	}
}

// TestContentEntityIDsStayStableAcrossAbsentAndPresentIncomingIDs pins the
// other half: a producer that supplies NO id must land on the same uid as one
// that supplies a stale id, or a replay writes a second node for a row it
// already wrote.
func TestContentEntityIDsStayStableAcrossAbsentAndPresentIncomingIDs(t *testing.T) {
	t.Parallel()

	for _, label := range []string{
		"TerraformBlock", "CloudFormationCondition", "CloudFormationImport",
		"CloudFormationExport", "PagerDutyDeclaration",
	} {
		withID := canonicalGraphEntityID(label, "repo-1", "a/b.yaml", "t", "n", 3, "stale-id")
		withoutID := canonicalGraphEntityID(label, "repo-1", "a/b.yaml", "t", "n", 3, "")
		if withID != withoutID {
			t.Errorf("%s: uid depends on whether the producer supplied an id (%q vs %q); "+
				"a replay would write a second node for a row it already wrote",
				label, withID, withoutID)
		}
	}
}
