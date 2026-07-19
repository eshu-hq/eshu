// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// cfnRow returns the row named wantName from payload[bucket], or fails the
// test. Shared by every CloudFormation real-line-number test below.
func cfnRow(t *testing.T, payload map[string]any, bucket string, wantName string) map[string]any {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", bucket, payload[bucket])
	}
	for _, item := range items {
		if name, _ := item["name"].(string); name == wantName {
			return item
		}
	}
	t.Fatalf("%s missing name %q in %#v", bucket, wantName, items)
	return nil
}

func assertCFNLines(t *testing.T, row map[string]any, wantLine int, wantEndLine int) {
	t.Helper()

	if got, want := row["line_number"], wantLine; got != want {
		t.Fatalf("line_number = %#v, want %d", got, want)
	}
	if got, want := row["end_line"], wantEndLine; got != want {
		t.Fatalf("end_line = %#v, want %d", got, want)
	}
}

// TestDefaultEngineParsePathYAMLCloudFormationVpcFixtureRealLines proves
// issue #5328's fix against the comprehensive vpc.yaml fixture: every
// CloudFormation entity now carries its own real, distinct source line_number
// and a real end_line spanning its value, instead of every entity in the
// document sharing the single document-root line the generic YAML decoder
// used to stamp on all of them (and never emitting end_line at all, which
// forced materialize.go's snippet window onto a fixed startLine..startLine+24
// fallback).
func TestDefaultEngineParsePathYAMLCloudFormationVpcFixtureRealLines(t *testing.T) {
	t.Parallel()

	fixtureDir, err := filepath.Abs(filepath.Join("..", "..", "..", "tests", "fixtures", "ecosystems", "cloudformation_comprehensive"))
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v, want nil", err)
	}
	filePath := filepath.Join(fixtureDir, "vpc.yaml")

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(fixtureDir, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertCFNLines(t, cfnRow(t, got, "cloudformation_parameters", "VpcCidr"), 5, 7)
	assertCFNLines(t, cfnRow(t, got, "cloudformation_resources", "VPC"), 10, 17)
	assertCFNLines(t, cfnRow(t, got, "cloudformation_resources", "PublicSubnet"), 19, 24)
	assertCFNLines(t, cfnRow(t, got, "cloudformation_resources", "PrivateSubnet"), 26, 31)
	assertCFNLines(t, cfnRow(t, got, "cloudformation_resources", "SecurityGroup"), 33, 42)
	assertCFNLines(t, cfnRow(t, got, "cloudformation_outputs", "VpcId"), 45, 46)
	assertCFNLines(t, cfnRow(t, got, "cloudformation_outputs", "PublicSubnetId"), 47, 48)
	assertCFNLines(t, cfnRow(t, got, "cloudformation_outputs", "PrivateSubnetId"), 49, 50)

	// Every resource's line_number must be distinct: the pre-fix bug stamped
	// the same document-root line (1) on all of them.
	seen := map[int]bool{}
	for _, name := range []string{"VPC", "PublicSubnet", "PrivateSubnet", "SecurityGroup"} {
		line := cfnRow(t, got, "cloudformation_resources", name)["line_number"].(int)
		if seen[line] {
			t.Fatalf("resource %q reused line_number %d already claimed by another resource", name, line)
		}
		seen[line] = true
	}
}

// TestDefaultEngineParsePathYAMLCloudFormationAnchorMergeKey proves the
// position walk resolves a `<<: *anchor` merge key the same way the existing
// document decode already does: the merge-injected entity (SharedBucket,
// defined under the ".Shared" anchor block, never itself a template section)
// is attributed to its own key's physical line at the anchor's definition
// site, not to the "<<" line inside Resources, and not to any fabricated
// per-entity guess. The explicit ExtraBucket entity keeps its own distinct
// line untouched by the merge.
func TestDefaultEngineParsePathYAMLCloudFormationAnchorMergeKey(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "merge-stack.yaml")
	writeTestFile(
		t,
		filePath,
		`AWSTemplateFormatVersion: "2010-09-09"
.Shared: &CommonResources
  SharedBucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: shared-bucket
Resources:
  <<: *CommonResources
  ExtraBucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: extra-bucket
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertCFNLines(t, cfnRow(t, got, "cloudformation_resources", "SharedBucket"), 3, 6)
	assertCFNLines(t, cfnRow(t, got, "cloudformation_resources", "ExtraBucket"), 9, 12)
}

// TestDefaultEngineParsePathYAMLCloudFormationMultiDocumentStream proves the
// position walk handles a multi-document `---` stream without crashing or
// mis-attributing lines: gopkg.in/yaml.v3 Node.Line is file-absolute across
// document boundaries, and the second document's CloudFormation entities
// must carry their real file-absolute lines, not lines relative to where
// their own document happens to start.
func TestDefaultEngineParsePathYAMLCloudFormationMultiDocumentStream(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "multi-doc.yaml")
	writeTestFile(
		t,
		filePath,
		`---
SomeOtherDoc: true
---
AWSTemplateFormatVersion: "2010-09-09"
Resources:
  Bucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: doc2-bucket
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertCFNLines(t, cfnRow(t, got, "cloudformation_resources", "Bucket"), 6, 9)
}

// TestDefaultEngineParsePathYAMLCloudFormationNestedSameNameKey proves the
// position walk anchors strictly at the document root's own top-level pairs:
// a Stack-in-Stack resource (AWS::CloudFormation::Stack) whose own Properties
// happens to nest a key literally named "Resources" must never be mistaken
// for the template's real Resources section. RealBucket, the second true
// top-level resource, must still get its own correct, independent position.
func TestDefaultEngineParsePathYAMLCloudFormationNestedSameNameKey(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "nested-stack.yaml")
	writeTestFile(
		t,
		filePath,
		`AWSTemplateFormatVersion: "2010-09-09"
Resources:
  NestedStack:
    Type: AWS::CloudFormation::Stack
    Properties:
      TemplateURL: https://example.com/nested.yaml
      Parameters:
        Resources:
          FakeInnerBucket: true
  RealBucket:
    Type: AWS::S3::Bucket
Outputs:
  Ignored:
    Value: ok
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	resources := got["cloudformation_resources"].([]map[string]any)
	if len(resources) != 2 {
		t.Fatalf("cloudformation_resources = %#v, want exactly 2 rows (NestedStack, RealBucket) -- a nested same-named key must not be treated as a top-level entity", resources)
	}
	assertCFNLines(t, cfnRow(t, got, "cloudformation_resources", "NestedStack"), 3, 9)
	assertCFNLines(t, cfnRow(t, got, "cloudformation_resources", "RealBucket"), 10, 11)
}

// TestDefaultEngineParsePathJSONCloudFormationPinsDocumentRootLines pins the
// documented, unfixed JSON CloudFormation behavior (issue #5348, deferred):
// JSON decoding does not preserve per-key positions, so every entity in a
// JSON CloudFormation template still reports the single document-root
// line_number (1) and never sets end_line. This test is the ready red test
// for #5348 -- it must start failing the moment JSON gains real per-entity
// positions, so that fix does not silently drift from its own documented
// contract.
func TestDefaultEngineParsePathJSONCloudFormationPinsDocumentRootLines(t *testing.T) {
	t.Parallel()

	fixtureDir, err := filepath.Abs(filepath.Join("..", "..", "..", "tests", "fixtures", "ecosystems", "cloudformation_comprehensive"))
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v, want nil", err)
	}
	filePath := filepath.Join(fixtureDir, "stack.json")

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(fixtureDir, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	for _, tc := range []struct {
		bucket string
		name   string
	}{
		{bucket: "cloudformation_parameters", name: "InstanceType"},
		{bucket: "cloudformation_resources", name: "WebServer"},
		{bucket: "cloudformation_outputs", name: "InstanceId"},
	} {
		row := cfnRow(t, got, tc.bucket, tc.name)
		if got, want := row["line_number"], 1; got != want {
			t.Fatalf("%s[%s].line_number = %#v, want %d (document-root fallback, JSON positions tracked in #5348)", tc.bucket, tc.name, got, want)
		}
		if _, hasEndLine := row["end_line"]; hasEndLine {
			t.Fatalf("%s[%s] has end_line = %#v, want no end_line field (JSON Parse never sets one)", tc.bucket, tc.name, row["end_line"])
		}
	}
}
