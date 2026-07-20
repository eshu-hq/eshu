// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shape

import (
	"strconv"
	"strings"
	"testing"
)

// cfnLinesTestBody builds a synthetic CloudFormation-shaped file body whose
// real template content ends at line 6 (a single VPC resource) but whose
// total file is padded out to 30 lines with unrelated trailing content, so a
// startLine+24 fallback window (24 lines past line 3, the entity's
// LineNumber) would clearly overrun the entity's real 4-line span into that
// padding.
func cfnLinesTestBody() string {
	lines := []string{
		`AWSTemplateFormatVersion: "2010-09-09"`, // 1
		"Resources:",                             // 2
		"  VPC:",                                 // 3
		"    Type: AWS::EC2::VPC",                // 4
		"    Properties:",                        // 5
		"      CidrBlock: 10.0.0.0/16",           // 6
	}
	for len(lines) < 30 {
		lines = append(lines, "# padding line "+strconv.Itoa(len(lines)+1))
	}
	return strings.Join(lines, "\n") + "\n"
}

// TestMaterializeCloudFormationEntityUsesRealEndLineNotStartPlus24 proves
// issue #5328's downstream half of the fix: entityEndLine (materialize.go)
// already supported a real per-entity EndLine field before this issue -- the
// bug was entirely that cloudformation.Parse never populated one. Feeding it
// a realistic CloudFormation-shaped LineNumber/EndLine pair (as
// cloudformation.ParseWithPositions now does for YAML) must produce a
// SourceCache snippet that tracks the entity's real 4-line span (3..6), not
// the fixed startLine..startLine+24 window materialize.go falls back to when
// EndLine is absent/invalid and there is no next entity to bound it.
func TestMaterializeCloudFormationEntityUsesRealEndLineNotStartPlus24(t *testing.T) {
	t.Parallel()

	body := cfnLinesTestBody()
	got, err := Materialize(Input{
		RepoID: "repository:r_12345678",
		Files: []File{
			{
				Path: "stack.yaml",
				Body: body,
				EntityBuckets: map[string][]Entity{
					"cloudformation_resources": {
						{
							Name:       "VPC",
							LineNumber: 3,
							EndLine:    6,
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}

	if len(got.Entities) != 1 {
		t.Fatalf("len(Materialize().Entities) = %d, want 1", len(got.Entities))
	}
	entity := got.Entities[0]
	if got, want := entity.EntityType, "CloudFormationResource"; got != want {
		t.Fatalf("entity.EntityType = %q, want %q", got, want)
	}
	if got, want := entity.StartLine, 3; got != want {
		t.Fatalf("entity.StartLine = %d, want %d", got, want)
	}
	if got, want := entity.EndLine, 6; got != want {
		t.Fatalf("entity.EndLine = %d, want %d (real span, not startLine+24=27)", got, want)
	}

	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	wantSnippet := strings.Join(lines[2:6], "\n") + "\n" // 0-indexed lines 3..6
	if got, want := entity.SourceCache, wantSnippet; got != want {
		t.Fatalf("entity.SourceCache = %q, want %q (real span, not padded through startLine+24)", got, want)
	}
	if strings.Contains(entity.SourceCache, "padding line") {
		t.Fatalf("entity.SourceCache = %q, must not include the startLine+24 padding fallback would have captured", entity.SourceCache)
	}
}

// TestMaterializeCloudFormationEntityFallsBackWithoutEndLine pins the
// pre-#5328 fallback behavior for an entity that still has no real EndLine
// (EndLine 0, which is < LineNumber and so treated as absent): with no
// subsequent entity to bound it, materialize.go's entityEndLine falls back
// to startLine+24. This is the exact degraded shape every CloudFormation
// entity had before the fix; both the YAML (#5328) and JSON (#5348) adapters
// now supply a real EndLine on the happy path, but the fallback path still
// runs for any entity that reaches materialize without one (a document-level
// position walk failure in either adapter), so it is kept as a regression pin
// so the fallback path itself is not accidentally removed.
func TestMaterializeCloudFormationEntityFallsBackWithoutEndLine(t *testing.T) {
	t.Parallel()

	body := cfnLinesTestBody()
	got, err := Materialize(Input{
		RepoID: "repository:r_12345678",
		Files: []File{
			{
				Path: "stack.yaml",
				Body: body,
				EntityBuckets: map[string][]Entity{
					"cloudformation_resources": {
						{
							Name:       "VPC",
							LineNumber: 3,
							// EndLine intentionally left at the zero value,
							// matching an entity that reached materialize with
							// no measured span (the zero-Positions Parse path,
							// or a document-level walk failure in either
							// adapter).
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}
	if len(got.Entities) != 1 {
		t.Fatalf("len(Materialize().Entities) = %d, want 1", len(got.Entities))
	}
	entity := got.Entities[0]
	if got, want := entity.StartLine, 3; got != want {
		t.Fatalf("entity.StartLine = %d, want %d", got, want)
	}
	if got, want := entity.EndLine, 27; got != want {
		t.Fatalf("entity.EndLine = %d, want %d (startLine+24 fallback)", got, want)
	}
}
