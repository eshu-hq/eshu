// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Regressions for the recursive decode-usage scan introduced by the package
// restructure (#6053/#6055): a handler that moves into a subpackage must stay
// visible, two packages' same-named symbols must not merge, and a testdata
// fixture must never count as production usage. Kept separate from
// usage_test.go so the core ScanDecodeUsage cases and these restructure
// guards each stay well under the 500-line cap.
package payloadusage

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScanDecodeUsageFindsUsageInSubdirectory is the restructure guard: a
// handler that moves into a subpackage must stay visible to the manifest gate.
// parseReducerDir originally read only the top level (os.ReadDir) on the stated
// assumption that "go/internal/reducer is flat, no subpackages" -- an assumption
// that is already false (reducer has dsl/, tfstate/ and tags/) and that a
// restructure makes false everywhere. A decode site the scan cannot see is a
// silent under-report, not an error: the gate stays green while covering less.
func TestScanDecodeUsageFindsUsageInSubdirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sub := filepath.Join(dir, "containerimage")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatalf("mkdir subpackage: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "extract_rows.go"), []byte(fixtureHandlerFile), 0o600); err != nil {
		t.Fatalf("write fixture in subpackage: %v", err)
	}

	seams := []DecodeSeam{{FuncName: "decodeAWSResource", FactKindConst: "FactKindAWSResource", StructPackage: "awsv1", StructName: "Resource"}}
	usage, err := ScanDecodeUsage(dir, seams, nil)
	if err != nil {
		t.Fatalf("ScanDecodeUsage() error = %v", err)
	}

	if len(usage["decodeAWSResource"]) == 0 {
		t.Fatalf("no usage recorded for a decode site one directory down; the scan is not recursive, so a family that moves into a subpackage drops out of the manifest silently. got %+v", usage)
	}
}

// fixtureRootWrapper and fixtureSubWrapper both declare a struct literally
// named "resultWrapper" with a field literally named "payload", each typed as
// a DIFFERENT real seam struct (awsv1.Resource in the root package,
// tagsv1.Tag in the "othertool" subpackage). Go permits two distinct packages
// to declare a type with the identical name; the recursive walk (#6055) reads
// both into one flat file list, and wrapperSeamFields keyed its result only by
// the unqualified type name ("resultWrapper"), so whichever package's
// declaration is walked last silently overwrote the other's entry in that
// shared map (review finding on #6080).
const fixtureRootWrapper = `package reducer

type resultWrapper struct {
	payload awsv1.Resource
}

func consumeRoot(w resultWrapper) {
	_ = w.payload.AccountID
}

func decodeAWSResource(env facts.Envelope) (awsv1.Resource, error) {
	return awsv1.Resource{}, nil
}
`

const fixtureSubWrapper = `package othertool

type resultWrapper struct {
	payload tagsv1.Tag
}

func consumeSub(w resultWrapper) {
	_ = w.payload.Key
}

func decodeAWSTag(env facts.Envelope) (tagsv1.Tag, error) {
	return tagsv1.Tag{}, nil
}
`

// TestScanDecodeUsageDoesNotClobberWrapperAcrossPackages is the regression
// guard for the wrapper-struct half of the cross-package collision finding:
// a wrapper type name is not unique across packages, so wrapperSeamFields
// must derive its type->field->seam mapping separately per package directory
// instead of folding every parsed file into one shared, unqualified-name-keyed
// map.
func TestScanDecodeUsageDoesNotClobberWrapperAcrossPackages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sub := filepath.Join(dir, "othertool")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatalf("mkdir subpackage: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "root_handler.go"), []byte(fixtureRootWrapper), 0o600); err != nil {
		t.Fatalf("write root fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "sub_handler.go"), []byte(fixtureSubWrapper), 0o600); err != nil {
		t.Fatalf("write sub fixture: %v", err)
	}

	seams := []DecodeSeam{
		{FuncName: "decodeAWSResource", FactKindConst: "FactKindAWSResource", StructPackage: "awsv1", StructName: "Resource"},
		{FuncName: "decodeAWSTag", FactKindConst: "FactKindAWSTag", StructPackage: "tagsv1", StructName: "Tag"},
	}
	usage, err := ScanDecodeUsage(dir, seams, nil)
	if err != nil {
		t.Fatalf("ScanDecodeUsage() error = %v", err)
	}

	resourceFields := map[string]bool{}
	for _, e := range usage["decodeAWSResource"] {
		resourceFields[e.GoFieldName] = true
	}
	tagFields := map[string]bool{}
	for _, e := range usage["decodeAWSTag"] {
		tagFields[e.GoFieldName] = true
	}

	if !resourceFields["AccountID"] {
		t.Errorf("decodeAWSResource usage = %+v, want AccountID (read through the root package's own resultWrapper)", usage["decodeAWSResource"])
	}
	if resourceFields["Key"] {
		t.Errorf("decodeAWSResource usage = %+v, must not contain Key: that read belongs to the othertool package's unrelated resultWrapper, wrongly attributed via the shared type-name map", usage["decodeAWSResource"])
	}
	if !tagFields["Key"] {
		t.Errorf("decodeAWSTag usage = %+v, want Key (read through the othertool package's own resultWrapper)", usage["decodeAWSTag"])
	}
	if tagFields["AccountID"] {
		t.Errorf("decodeAWSTag usage = %+v, must not contain AccountID: that read belongs to the root package's unrelated resultWrapper, wrongly attributed via the shared type-name map", usage["decodeAWSTag"])
	}
}

// fixtureRootDecodeFunc and fixtureSubDecodeFunc both declare a
// package-level function literally named "decodeAWSResource": the root
// package's is the real, registered seam; the "othertool" subpackage's is a
// coincidentally-same-named, unrelated, but still validly decode-shaped
// function (facts.Envelope param, (<pkg>.<Struct>, error) result) returning a
// completely different struct. Go permits this across packages; the scan's
// decodeFuncs set is keyed only by the bare function name, so before the fix
// the subpackage's call site was also bound to the real seam.
const fixtureRootDecodeFunc = `package reducer

func consumeRoot(env facts.Envelope) {
	resource, _ := decodeAWSResource(env)
	_ = resource.AccountID
}

func decodeAWSResource(env facts.Envelope) (awsv1.Resource, error) {
	return awsv1.Resource{}, nil
}
`

const fixtureSubDecodeFunc = `package othertool

func consumeSub(env facts.Envelope) {
	resource, _ := decodeAWSResource(env)
	_ = resource.Unrelated
}

func decodeAWSResource(env facts.Envelope) (otherv1.Widget, error) {
	return otherv1.Widget{}, nil
}
`

// TestScanDecodeUsageDoesNotMisattributeDecodeCallAcrossPackages is the
// regression guard for the decode-function half of the cross-package
// collision finding: a bare function name is not unique across packages, so a
// package's own, differently-shaped, same-named function must not satisfy a
// real seam's binding for that package's call sites.
func TestScanDecodeUsageDoesNotMisattributeDecodeCallAcrossPackages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sub := filepath.Join(dir, "othertool")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatalf("mkdir subpackage: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "root_handler.go"), []byte(fixtureRootDecodeFunc), 0o600); err != nil {
		t.Fatalf("write root fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "sub_handler.go"), []byte(fixtureSubDecodeFunc), 0o600); err != nil {
		t.Fatalf("write sub fixture: %v", err)
	}

	seams := []DecodeSeam{
		{FuncName: "decodeAWSResource", FactKindConst: "FactKindAWSResource", StructPackage: "awsv1", StructName: "Resource"},
	}
	usage, err := ScanDecodeUsage(dir, seams, nil)
	if err != nil {
		t.Fatalf("ScanDecodeUsage() error = %v", err)
	}

	fields := map[string]bool{}
	for _, e := range usage["decodeAWSResource"] {
		fields[e.GoFieldName] = true
	}
	if !fields["AccountID"] {
		t.Errorf("decodeAWSResource usage = %+v, want AccountID (the real seam's own call site)", usage["decodeAWSResource"])
	}
	if fields["Unrelated"] {
		t.Errorf("decodeAWSResource usage = %+v, must not contain Unrelated: that read belongs to the othertool package's own unrelated decodeAWSResource function, wrongly bound to the real seam by bare function name", usage["decodeAWSResource"])
	}
}

// TestScanDecodeUsageSkipsTestdata pins the guard that landed in the same commit
// that made parseReducerDir recursive. testdata holds deliberately broken or
// illustrative fixtures; parsing one as production source would count its field
// reads as real usage in the manifest, masking a field that nothing in
// production actually reads. globFilesRecursive has the mirror of this test in
// load_test.go; this is the one for the AST walk.
func TestScanDecodeUsageSkipsTestdata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	td := filepath.Join(dir, "testdata")
	if err := os.MkdirAll(td, 0o750); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}
	// A fixture that WOULD register usage if the walk read it.
	if err := os.WriteFile(filepath.Join(td, "fixture.go"), []byte(fixtureHandlerFile), 0o600); err != nil {
		t.Fatalf("write testdata fixture: %v", err)
	}

	seams := []DecodeSeam{{FuncName: "decodeAWSResource", FactKindConst: "FactKindAWSResource", StructPackage: "awsv1", StructName: "Resource"}}
	usage, err := ScanDecodeUsage(dir, seams, nil)
	if err != nil {
		t.Fatalf("ScanDecodeUsage() error = %v", err)
	}
	if len(usage["decodeAWSResource"]) != 0 {
		t.Fatalf("usage = %+v, want none; a testdata fixture must never count as production decode usage", usage)
	}
}
