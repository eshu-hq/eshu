// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import "testing"

// TestCloudformationMaxLineAliasValuedEntityNotInflatedByAnchorSpan pins the
// invariant documented on cloudformationMaxLine: an entity whose entire value
// is a bare `*anchor` alias gets StartLine == EndLine == its own key line,
// never inflated by walking into the (possibly much larger) subtree the
// anchor itself was defined with. Real's Properties anchor spans lines 4-10;
// Mirror's value is exactly that alias, so Mirror's EndLine must stay pinned
// to its own line (11), not the anchor's span.
func TestCloudformationMaxLineAliasValuedEntityNotInflatedByAnchorSpan(t *testing.T) {
	t.Parallel()

	source := "Resources:\n" + // line 1
		"  Real:\n" + // line 2
		"    Type: AWS::S3::Bucket\n" + // line 3
		"    Properties: &sharedProps\n" + // line 4
		"      BucketName: shared-bucket\n" + // line 5
		"      Tags:\n" + // line 6
		"        - Key: Name\n" + // line 7
		"          Value: shared-bucket\n" + // line 8
		"        - Key: Environment\n" + // line 9
		"          Value: prod\n" + // line 10
		"  Mirror: *sharedProps\n" // line 11

	documents, nodes, err := decodeDocumentsWithNodes(source)
	if err != nil {
		t.Fatalf("decodeDocumentsWithNodes() error = %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("len(documents) = %d, want 1", len(documents))
	}
	document, ok := documents[0].(map[string]any)
	if !ok {
		t.Fatalf("documents[0] is %T, want map[string]any", documents[0])
	}
	delete(document, "__eshu_line_number")

	positions, fallbacks := cloudformationPositionsFromRoot(nodes[0], document)
	if len(fallbacks) != 0 {
		t.Fatalf("unexpected fallbacks: %#v", fallbacks)
	}

	real, ok := positions.Resources.Entries["Real"]
	if !ok {
		t.Fatalf("positions.Resources.Entries missing Real: %#v", positions.Resources.Entries)
	}
	if want := 2; real.StartLine != want {
		t.Fatalf("Real StartLine = %d, want %d", real.StartLine, want)
	}
	if want := 10; real.EndLine != want {
		t.Fatalf("Real EndLine = %d, want %d (the anchor's own span)", real.EndLine, want)
	}

	mirror, ok := positions.Resources.Entries["Mirror"]
	if !ok {
		t.Fatalf("positions.Resources.Entries missing Mirror: %#v", positions.Resources.Entries)
	}
	if want := 11; mirror.StartLine != want {
		t.Fatalf("Mirror StartLine = %d, want %d", mirror.StartLine, want)
	}
	if want := 11; mirror.EndLine != want {
		t.Fatalf("Mirror EndLine = %d, want %d (own key line, NOT inflated by the anchor's 2-10 span)", mirror.EndLine, want)
	}
}
