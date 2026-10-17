// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "testing"

// relationshipEvidenceTargetAttributableVerbs is the pinned 21-verb
// classification the contract owns: every verb in the relationships catalog
// with the target-attribution bit the catalog used to carry on
// relationshipVerbEntry.targetAttributable before #6642 promoted it here.
// The canonical mixed-case AWS_lambda_function_uses_image entry is listed
// in its stored case; the contract keys UPPER-CASED, so every casing of it
// must resolve the same way (#5450 P1-B).
var relationshipEvidenceTargetAttributableVerbs = []struct {
	verb string
	want bool
}{
	{verb: "CALLS", want: true},
	{verb: "IMPORTS", want: false},
	{verb: "INHERITS", want: true},
	{verb: "REFERENCES", want: true},
	{verb: "OVERRIDES", want: true},
	{verb: "QUERIES_TABLE", want: false},
	{verb: "DEPLOYS_FROM", want: true},
	{verb: "INSTANCE_OF", want: true},
	{verb: "RECONCILES_FROM", want: true},
	{verb: "PROVISIONS_DEPENDENCY_FOR", want: true},
	{verb: "USES_MODULE", want: true},
	{verb: "DISCOVERS_CONFIG_IN", want: true},
	{verb: "MANAGES", want: false},
	{verb: "ATLANTIS_DEPENDS_ON", want: false},
	{verb: "USES_WORKFLOW", want: false},
	{verb: "RUNS_ON", want: false},
	{verb: "AWS_lambda_function_uses_image", want: false},
	{verb: "DEPENDS_ON", want: true},
	{verb: "INVOKES_CLOUD_ACTION", want: false},
	{verb: "READS_CONFIG_FROM", want: true},
	{verb: "TAINT_FLOWS_TO", want: true},
}

// TestRelationshipVerbTargetAttributablePinsCatalog pins the scope-binding
// direction: the verb-level classifier answers every catalog verb exactly as
// the deleted relationshipVerbEntry.targetAttributable field did, so the
// relationships/edges scope predicate binds the target endpoint for the same
// verbs and renders byte-identical Cypher. Input casing and padding must not
// matter: the catalog's only mixed-case verb resolves through the
// UPPER-CASED key no matter how the caller spells it.
func TestRelationshipVerbTargetAttributablePinsCatalog(t *testing.T) {
	t.Parallel()
	if len(relationshipEvidenceTargetAttributableVerbs) != 21 {
		t.Fatalf("pinned verb table has %d entries, want 21 (one per catalog verb)", len(relationshipEvidenceTargetAttributableVerbs))
	}
	for _, tt := range relationshipEvidenceTargetAttributableVerbs {
		t.Run(tt.verb, func(t *testing.T) {
			t.Parallel()
			if got := RelationshipVerbTargetAttributable(tt.verb); got != tt.want {
				t.Fatalf("RelationshipVerbTargetAttributable(%q) = %v, want %v", tt.verb, got, tt.want)
			}
		})
	}
	for _, tt := range []struct {
		name string
		verb string
		want bool
	}{
		{name: "lowercase true verb", verb: "calls", want: true},
		{name: "lowercase false verb", verb: "imports", want: false},
		{name: "padded true verb", verb: "  CALLS ", want: true},
		{name: "mixed-case lambda verb canonical", verb: "AWS_lambda_function_uses_image", want: false},
		{name: "mixed-case lambda verb upper", verb: "AWS_LAMBDA_FUNCTION_USES_IMAGE", want: false},
		{name: "mixed-case lambda verb lower", verb: "aws_lambda_function_uses_image", want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := RelationshipVerbTargetAttributable(tt.verb); got != tt.want {
				t.Fatalf("RelationshipVerbTargetAttributable(%q) = %v, want %v", tt.verb, got, tt.want)
			}
		})
	}
}

// TestRelationshipVerbTargetAttributableUnknownVerbFailsClosed pins the
// scope-binding fallback: a verb outside the catalog reports
// target-unattributable, mirroring the deleted field's zero value. This is
// unreachable on the edges path (that endpoint 400s unknown verbs before any
// scope predicate renders), so the pin documents the default rather than a
// reachable branch.
func TestRelationshipVerbTargetAttributableUnknownVerbFailsClosed(t *testing.T) {
	t.Parallel()
	for _, verb := range []string{"SOME_FUTURE_VERB", "", "  "} {
		if got := RelationshipVerbTargetAttributable(verb); got {
			t.Fatalf("RelationshipVerbTargetAttributable(%q) = true, want false (unknown verbs stay source-only)", verb)
		}
	}
}

// relationshipEvidenceTargetAttributableRow builds a drilldown row carrying
// relationshipType with the given endpoint repo ids.
func relationshipEvidenceTargetAttributableRow(relationshipType, sourceRepo, targetRepo string) map[string]any {
	return map[string]any{
		"relationship_type": relationshipType,
		"source":            map[string]any{"repo_id": sourceRepo},
		"target":            map[string]any{"repo_id": targetRepo},
	}
}

// TestRelationshipEvidenceTargetAttributablePinsCatalog pins the 404-mask
// direction: the row-level classifier answers every catalog verb exactly as
// the old evidence.go predicate did. The classification dominates the row's
// own target repo_id: a known-false verb stays unattributable even when the
// row carries a target repo_id (source ownership alone authorizes), and a
// known-true verb stays attributable even when the target repo_id is empty
// (the target grant is then enforced and fails closed on "").
func TestRelationshipEvidenceTargetAttributablePinsCatalog(t *testing.T) {
	t.Parallel()
	for _, tt := range relationshipEvidenceTargetAttributableVerbs {
		t.Run(tt.verb, func(t *testing.T) {
			t.Parallel()
			attributableTarget := relationshipEvidenceTargetAttributableRow(tt.verb, "repo-a", "repo-b")
			if got := RelationshipEvidenceTargetAttributable(attributableTarget); got != tt.want {
				t.Fatalf("RelationshipEvidenceTargetAttributable(%q with target repo) = %v, want %v", tt.verb, got, tt.want)
			}
			globalTarget := relationshipEvidenceTargetAttributableRow(tt.verb, "repo-a", "")
			if got := RelationshipEvidenceTargetAttributable(globalTarget); got != tt.want {
				t.Fatalf("RelationshipEvidenceTargetAttributable(%q with global target) = %v, want %v", tt.verb, got, tt.want)
			}
		})
	}
}

// TestRelationshipEvidenceTargetAttributableUnknownVerbFallsBackToRow pins
// the 404-mask fallback byte-identically to the old predicate: an unknown
// verb resolves against the row's own target repo_id, so a non-empty target
// names a real repository that must stay protected while an empty one has no
// tenant secret and source ownership suffices.
func TestRelationshipEvidenceTargetAttributableUnknownVerbFallsBackToRow(t *testing.T) {
	t.Parallel()
	attributable := relationshipEvidenceTargetAttributableRow("SOME_FUTURE_VERB", "repo-a", "repo-b")
	if !RelationshipEvidenceTargetAttributable(attributable) {
		t.Fatal("RelationshipEvidenceTargetAttributable(unknown verb with target repo) = false, want true (fail closed)")
	}
	global := relationshipEvidenceTargetAttributableRow("SOME_FUTURE_VERB", "repo-a", "")
	if RelationshipEvidenceTargetAttributable(global) {
		t.Fatal("RelationshipEvidenceTargetAttributable(unknown verb with global target) = true, want false")
	}
}

// TestRelationshipEvidenceRowWithinAccessGuardsBothEndpoints pins the row
// gate over the contract predicates: unscoped callers pass, empty grants
// fail closed, both-endpoint grants pass, a missing target grant denies an
// attributable edge, and a source-owner grant alone suffices only for a
// global (unattributable) target.
func TestRelationshipEvidenceRowWithinAccessGuardsBothEndpoints(t *testing.T) {
	t.Parallel()
	granted := func(ids ...string) RepositoryAccessFilter {
		allowed := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			allowed[id] = struct{}{}
		}
		return RepositoryAccessFilter{AllowedRepositoryIDs: ids, Allowed: allowed}
	}
	for _, tt := range []struct {
		name   string
		row    map[string]any
		access RepositoryAccessFilter
		want   bool
	}{
		{
			name:   "unscoped caller passes",
			row:    relationshipEvidenceTargetAttributableRow("DEPLOYS_FROM", "repo-a", "repo-b"),
			access: RepositoryAccessFilter{AllScopes: true},
			want:   true,
		},
		{
			name:   "empty grant fails closed",
			row:    relationshipEvidenceTargetAttributableRow("DEPLOYS_FROM", "repo-a", "repo-b"),
			access: RepositoryAccessFilter{},
			want:   false,
		},
		{
			name:   "both endpoints granted passes",
			row:    relationshipEvidenceTargetAttributableRow("DEPLOYS_FROM", "repo-a", "repo-b"),
			access: granted("repo-a", "repo-b"),
			want:   true,
		},
		{
			name:   "missing target grant denies attributable edge",
			row:    relationshipEvidenceTargetAttributableRow("DEPLOYS_FROM", "repo-a", "repo-b"),
			access: granted("repo-a"),
			want:   false,
		},
		{
			name:   "missing source grant denies",
			row:    relationshipEvidenceTargetAttributableRow("DEPLOYS_FROM", "repo-a", "repo-b"),
			access: granted("repo-b"),
			want:   false,
		},
		{
			name:   "source owner reaches global target",
			row:    relationshipEvidenceTargetAttributableRow("IMPORTS", "repo-a", ""),
			access: granted("repo-a"),
			want:   true,
		},
		{
			name:   "non-source-owner denied global target",
			row:    relationshipEvidenceTargetAttributableRow("IMPORTS", "repo-a", ""),
			access: granted("repo-other"),
			want:   false,
		},
		{
			name:   "unknown verb with attributable target requires target grant",
			row:    relationshipEvidenceTargetAttributableRow("SOME_FUTURE_VERB", "repo-a", "repo-b"),
			access: granted("repo-a"),
			want:   false,
		},
		{
			name:   "unknown verb with global target admits source owner",
			row:    relationshipEvidenceTargetAttributableRow("SOME_FUTURE_VERB", "repo-a", ""),
			access: granted("repo-a"),
			want:   true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := RelationshipEvidenceRowWithinAccess(tt.row, tt.access); got != tt.want {
				t.Fatalf("RelationshipEvidenceRowWithinAccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRelationshipEvidenceEndpointRepoIDReadsEndpointRepo pins the endpoint
// reader: source/target repo ids come off the endpoint maps, and a missing,
// nil, or non-map endpoint reads as "" (which never matches a grant).
func TestRelationshipEvidenceEndpointRepoIDReadsEndpointRepo(t *testing.T) {
	t.Parallel()
	row := relationshipEvidenceTargetAttributableRow("CALLS", "repo-a", "repo-b")
	if got := RelationshipEvidenceEndpointRepoID(row, "source"); got != "repo-a" {
		t.Fatalf("RelationshipEvidenceEndpointRepoID(source) = %q, want %q", got, "repo-a")
	}
	if got := RelationshipEvidenceEndpointRepoID(row, "target"); got != "repo-b" {
		t.Fatalf("RelationshipEvidenceEndpointRepoID(target) = %q, want %q", got, "repo-b")
	}
	if got := RelationshipEvidenceEndpointRepoID(map[string]any{}, "source"); got != "" {
		t.Fatalf("RelationshipEvidenceEndpointRepoID(missing) = %q, want %q", got, "")
	}
	if got := RelationshipEvidenceEndpointRepoID(map[string]any{"source": nil}, "source"); got != "" {
		t.Fatalf("RelationshipEvidenceEndpointRepoID(nil) = %q, want %q", got, "")
	}
	if got := RelationshipEvidenceEndpointRepoID(map[string]any{"source": "repo-a"}, "source"); got != "" {
		t.Fatalf("RelationshipEvidenceEndpointRepoID(non-map) = %q, want %q", got, "")
	}
}
