// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import "testing"

// TestCloudformationSectionNodesDuplicateTopLevelResourcesKeepsLastMatch pins
// issue #5328's follow-up fix: a malformed template with two top-level
// "Resources:" blocks must have its node walk agree with the flattened
// document DecodeDocuments actually produced. node_decode.go's
// yamlMappingNodeToAny does `result[key] = value` unconditionally on a
// duplicate key (last-wins), so the flattened document keeps only the SECOND
// "Resources:" block. cloudformationSectionNodes must therefore anchor at
// that same SECOND block -- not the first -- or the position walk resolves
// entity names against the wrong node subtree and every resource in the
// (only) surviving section falls back to a stale, wrong section-header line.
func TestCloudformationSectionNodesDuplicateTopLevelResourcesKeepsLastMatch(t *testing.T) {
	t.Parallel()

	source := "AWSTemplateFormatVersion: '2010-09-09'\n" +
		"Resources:\n" + // line 2 -- FIRST (stale, must be ignored)
		"  FirstBucket:\n" + // line 3
		"    Type: AWS::S3::Bucket\n" + // line 4
		"Outputs:\n" + // line 5
		"  Foo:\n" + // line 6
		"    Value: bar\n" + // line 7
		"Resources:\n" + // line 8 -- SECOND (the one the flatten actually kept)
		"  SecondBucket:\n" + // line 9
		"    Type: AWS::S3::Bucket\n" // line 10

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

	resourcesSection, ok := document["Resources"].(map[string]any)
	if !ok {
		t.Fatalf(`document["Resources"] is %T, want map[string]any`, document["Resources"])
	}
	if _, ok := resourcesSection["SecondBucket"]; !ok {
		t.Fatalf("flattened Resources section missing SecondBucket (last-wins flatten broke): %#v", resourcesSection)
	}
	if _, ok := resourcesSection["FirstBucket"]; ok {
		t.Fatalf("flattened Resources section unexpectedly kept FirstBucket -- last-wins overwrite should have dropped it: %#v", resourcesSection)
	}

	positions, fallbacks := cloudformationPositionsFromRoot(nodes[0], document)

	for _, fallback := range fallbacks {
		if fallback.Section == "Resources" && fallback.Reason == "entity_position_missing" {
			t.Fatalf(
				"unexpected entity_position_missing fallback for Resources: %#v "+
					"(node walk anchored at the stale FIRST Resources block instead of the "+
					"LAST one the flattened document agrees with)",
				fallback,
			)
		}
	}

	got, ok := positions.Resources.Entries["SecondBucket"]
	if !ok {
		t.Fatalf("positions.Resources.Entries missing SecondBucket: %#v", positions.Resources.Entries)
	}
	if want := 9; got.StartLine != want {
		t.Fatalf("SecondBucket StartLine = %d, want %d (the SECOND \"Resources:\" block's real line)", got.StartLine, want)
	}
	if want := 10; got.EndLine != want {
		t.Fatalf("SecondBucket EndLine = %d, want %d", got.EndLine, want)
	}
	if want := 8; positions.Resources.FallbackLine != want {
		t.Fatalf("Resources FallbackLine = %d, want %d (the SECOND section header's own line)", positions.Resources.FallbackLine, want)
	}
}
