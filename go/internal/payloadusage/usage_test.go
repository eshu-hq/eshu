// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"sort"
	"testing"
)

const fixtureHandlerFile = `package reducer

func extractResourceRows(env facts.Envelope) {
	resource, err := decodeAWSResource(env)
	if err != nil {
		return
	}
	_ = resource.AccountID
	_ = resource.ResourceType
	arn := resource.ARN
	_ = arn
}

func unrelatedHelper(x int) int {
	return x + 1
}

func decodeAWSResource(env facts.Envelope) (Resource, error) {
	// the seam itself; not a call site.
	return Resource{}, nil
}
`

const fixtureSecondHandlerFile = `package reducer

func joinRelationship(env facts.Envelope) {
	resource, err := decodeAWSResource(env)
	if err != nil {
		return
	}
	_ = resource.AccountID
	_ = resource.Name
}
`

func TestScanDecodeUsage(t *testing.T) {
	t.Parallel()

	dir := writeFixtureDir(t, map[string]string{
		"extract_rows.go":      fixtureHandlerFile,
		"join_relationship.go": fixtureSecondHandlerFile,
		"extract_rows_test.go": `package reducer

func TestSomething(t *T) {
	resource, _ := decodeAWSResource(env)
	_ = resource.ShouldNotAppear
}
`,
	})

	seams := []DecodeSeam{{FuncName: "decodeAWSResource", FactKindConst: "FactKindAWSResource", StructPackage: "awsv1", StructName: "Resource"}}
	usage, err := ScanDecodeUsage(dir, seams)
	if err != nil {
		t.Fatalf("ScanDecodeUsage() error = %v", err)
	}

	entries, ok := usage["decodeAWSResource"]
	if !ok {
		t.Fatalf("no usage recorded for decodeAWSResource; got %+v", usage)
	}

	fields := map[string][]string{} // GoFieldName -> files
	for _, e := range entries {
		fields[e.GoFieldName] = append(fields[e.GoFieldName], e.File)
	}
	for name := range fields {
		sort.Strings(fields[name])
	}

	wantFields := map[string][]string{
		"AccountID":    {"extract_rows.go", "join_relationship.go"},
		"ResourceType": {"extract_rows.go"},
		"ARN":          {"extract_rows.go"},
		"Name":         {"join_relationship.go"},
	}
	for field, wantFiles := range wantFields {
		gotFiles, ok := fields[field]
		if !ok {
			t.Errorf("field %q not found in usage; got %+v", field, fields)
			continue
		}
		if len(gotFiles) != len(wantFiles) {
			t.Errorf("field %q files = %v, want %v", field, gotFiles, wantFiles)
			continue
		}
		for i := range wantFiles {
			if gotFiles[i] != wantFiles[i] {
				t.Errorf("field %q files = %v, want %v", field, gotFiles, wantFiles)
				break
			}
		}
	}

	if _, ok := fields["ShouldNotAppear"]; ok {
		t.Error("a _test.go file's usage leaked into ScanDecodeUsage's output; test files must be excluded")
	}
}

func TestScanDecodeUsageIgnoresUnboundSelectors(t *testing.T) {
	t.Parallel()

	// A selector on a variable that was never assigned from a decode call
	// (e.g. a plain struct literal) must not be attributed to the seam.
	dir := writeFixtureDir(t, map[string]string{
		"unrelated.go": `package reducer

func buildSomethingElse() {
	other := SomeOtherStruct{}
	_ = other.Field
}
`,
	})

	seams := []DecodeSeam{{FuncName: "decodeAWSResource", FactKindConst: "FactKindAWSResource", StructPackage: "awsv1", StructName: "Resource"}}
	usage, err := ScanDecodeUsage(dir, seams)
	if err != nil {
		t.Fatalf("ScanDecodeUsage() error = %v", err)
	}
	if len(usage["decodeAWSResource"]) != 0 {
		t.Fatalf("usage = %+v, want none: no variable in this fixture is bound to decodeAWSResource", usage["decodeAWSResource"])
	}
}

func TestScanDecodeUsageMissingDirErrors(t *testing.T) {
	t.Parallel()

	_, err := ScanDecodeUsage("/nonexistent/dir/for/sure", nil)
	if err == nil {
		t.Fatal("ScanDecodeUsage() error = nil, want an error for a missing directory")
	}
}

// fixtureCrossFunctionHandler mirrors the real
// s3_internet_exposure_rows.go pattern this test guards against a
// regression on: the decoded struct is passed BY VALUE into a helper
// function typed with the qualified struct name, not read directly in the
// same function body as the decode call. Before the parameter-binding fix,
// posture.PolicyGrantsPublic and posture.RestrictPublicBuckets were silently
// missing from the manifest because they are read inside
// deriveDecision/derivePublicPolicyDecision, two frames away from
// decodeS3BucketPosture.
const fixtureCrossFunctionHandler = `package reducer

func sortedPostures(env facts.Envelope) {
	posture, err := decodeS3BucketPosture(env)
	if err != nil {
		return
	}
	deriveDecision(posture)
}

func deriveDecision(posture awsv1.S3BucketPosture) {
	policyPublic := posture.PolicyGrantsPublic
	if policyPublic != nil && *policyPublic {
		derivePublicPolicyDecision(posture)
	}
}

func derivePublicPolicyDecision(posture awsv1.S3BucketPosture) {
	_ = posture.RestrictPublicBuckets
}

func decodeS3BucketPosture(env facts.Envelope) (awsv1.S3BucketPosture, error) {
	return awsv1.S3BucketPosture{}, nil
}
`

func TestScanDecodeUsageFollowsStructValuePassedToHelperFunction(t *testing.T) {
	t.Parallel()

	dir := writeFixtureDir(t, map[string]string{
		"s3_internet_exposure_rows.go": fixtureCrossFunctionHandler,
	})

	seams := []DecodeSeam{{
		FuncName:      "decodeS3BucketPosture",
		FactKindConst: "FactKindS3BucketPosture",
		StructPackage: "awsv1",
		StructName:    "S3BucketPosture",
	}}
	usage, err := ScanDecodeUsage(dir, seams)
	if err != nil {
		t.Fatalf("ScanDecodeUsage() error = %v", err)
	}

	entries := usage["decodeS3BucketPosture"]
	fieldNames := map[string]bool{}
	for _, e := range entries {
		fieldNames[e.GoFieldName] = true
	}

	for _, want := range []string{"PolicyGrantsPublic", "RestrictPublicBuckets"} {
		if !fieldNames[want] {
			t.Errorf("field %q read inside a helper function (not the decode call site itself) was not attributed to decodeS3BucketPosture; got %+v", want, entries)
		}
	}
}

// TestScanDecodeUsageDoesNotBindUnqualifiedParameterType proves the
// parameter-binding path requires a package-qualified type (awsv1.Resource),
// not a bare local type name, so a same-named local struct in a different
// package cannot be misattributed to a seam.
func TestScanDecodeUsageDoesNotBindUnqualifiedParameterType(t *testing.T) {
	t.Parallel()

	dir := writeFixtureDir(t, map[string]string{
		"unrelated_helper.go": `package reducer

func helper(resource Resource) {
	_ = resource.SomeField
}
`,
	})

	seams := []DecodeSeam{{FuncName: "decodeAWSResource", FactKindConst: "FactKindAWSResource", StructPackage: "awsv1", StructName: "Resource"}}
	usage, err := ScanDecodeUsage(dir, seams)
	if err != nil {
		t.Fatalf("ScanDecodeUsage() error = %v", err)
	}
	if len(usage["decodeAWSResource"]) != 0 {
		t.Fatalf("usage = %+v, want none: the helper's parameter type \"Resource\" is unqualified, not \"awsv1.Resource\"", usage["decodeAWSResource"])
	}
}
