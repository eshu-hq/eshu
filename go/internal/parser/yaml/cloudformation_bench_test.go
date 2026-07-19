// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildRepresentativeCloudFormationTemplate returns a CloudFormation/SAM
// template body with the given number of resources, parameters, outputs, and
// conditions. Every resource carries a nested Properties block (tags,
// ingress rules) and every other resource DependsOn its predecessor, so the
// value subtree the position walk's cloudformationMaxLine has to traverse is
// realistically deep rather than a single flat line -- the shape issue #5328
// measures, not a synthetic minimal case.
func buildRepresentativeCloudFormationTemplate(resources, params, outputs, conditions int) string {
	var b strings.Builder
	b.WriteString("AWSTemplateFormatVersion: '2010-09-09'\n")
	b.WriteString("Description: Representative benchmark stack for issue #5328\n")
	b.WriteString("Parameters:\n")
	for i := 0; i < params; i++ {
		fmt.Fprintf(&b, "  Param%02d:\n", i)
		fmt.Fprintf(&b, "    Type: String\n")
		fmt.Fprintf(&b, "    Default: value-%02d\n", i)
		fmt.Fprintf(&b, "    Description: Benchmark parameter %02d\n", i)
	}
	b.WriteString("Conditions:\n")
	for i := 0; i < conditions; i++ {
		fmt.Fprintf(&b, "  Cond%02d: !Equals [!Ref Param%02d, value-%02d]\n", i, i%maxInt(params, 1), i)
	}
	b.WriteString("Resources:\n")
	for i := 0; i < resources; i++ {
		fmt.Fprintf(&b, "  Bucket%03d:\n", i)
		fmt.Fprintf(&b, "    Type: AWS::S3::Bucket\n")
		fmt.Fprintf(&b, "    Properties:\n")
		fmt.Fprintf(&b, "      BucketName: bucket-%03d\n", i)
		fmt.Fprintf(&b, "      Tags:\n")
		fmt.Fprintf(&b, "        - Key: Name\n")
		fmt.Fprintf(&b, "          Value: bucket-%03d\n", i)
		fmt.Fprintf(&b, "        - Key: Environment\n")
		fmt.Fprintf(&b, "          Value: benchmark\n")
		fmt.Fprintf(&b, "      VersioningConfiguration:\n")
		fmt.Fprintf(&b, "        Status: Enabled\n")
		if conditions > 0 {
			fmt.Fprintf(&b, "    Condition: Cond%02d\n", i%conditions)
		}
		if i > 0 {
			fmt.Fprintf(&b, "    DependsOn: Bucket%03d\n", i-1)
		}
	}
	b.WriteString("Outputs:\n")
	for i := 0; i < outputs; i++ {
		fmt.Fprintf(&b, "  Output%02d:\n", i)
		fmt.Fprintf(&b, "    Description: Benchmark output %02d\n", i)
		fmt.Fprintf(&b, "    Value: !Ref Bucket%03d\n", i%maxInt(resources, 1))
		fmt.Fprintf(&b, "    Export:\n")
		fmt.Fprintf(&b, "      Name: !Sub '${AWS::StackName}-Output%02d'\n", i)
	}
	return b.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// benchmarkParseCloudFormationTemplate drives the stable public Parse()
// entrypoint over a generated CloudFormation template of the given shape, so
// the same benchmark body runs unchanged against origin/main (single
// DecodeDocuments pass, document-root line_number only) and this branch (the
// added decodeDocumentNodes second pass plus the position walk). See issue
// #5328 Performance Evidence for the before/after ns/op, B/op, and
// allocs/op comparison recorded via a saved origin/main worktree.
func benchmarkParseCloudFormationTemplate(b *testing.B, resources, params, outputs, conditions int) {
	b.Helper()
	dir := b.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	source := buildRepresentativeCloudFormationTemplate(resources, params, outputs, conditions)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		b.Fatalf("write stack.yaml: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		payload, err := Parse(path, false, Options{})
		if err != nil {
			b.Fatalf("Parse() error = %v", err)
		}
		if got := len(payload["cloudformation_resources"].([]map[string]any)); got != resources {
			b.Fatalf("cloudformation_resources rows = %d, want %d", got, resources)
		}
	}
}

// BenchmarkParseCloudFormationTemplateRepresentative measures Parse() over a
// mid-size template (100 resources, 20 parameters, 15 outputs, 10
// conditions) -- the common-case shape for issue #5328's added
// decodeDocumentNodes second decode pass.
func BenchmarkParseCloudFormationTemplateRepresentative(b *testing.B) {
	benchmarkParseCloudFormationTemplate(b, 100, 20, 15, 10)
}

// BenchmarkParseCloudFormationTemplateLarge measures Parse() over a
// worst-case-partition template (500 resources, 40 parameters, 40 outputs,
// 20 conditions) to confirm the added second decode pass does not degrade
// superlinearly on large templates.
func BenchmarkParseCloudFormationTemplateLarge(b *testing.B) {
	benchmarkParseCloudFormationTemplate(b, 500, 40, 40, 20)
}
