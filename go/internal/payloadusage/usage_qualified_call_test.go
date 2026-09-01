// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import "testing"

// fixtureQualifiedDecodeCallHandler mirrors the shape #6061's
// schemadecode/AGENTS.md instructs family packages to use once a decode seam
// has moved into the schemadecode subpackage: "a family package should
// import this package directly" rather than going through a root-level
// unqualified forwarder. The call site is package-qualified
// (schemadecode.DecodeContainerImageIdentity(env)), not the bare
// decodeContainerImageIdentity(env) shape recordDecodeBindings originally
// recognized.
const fixtureQualifiedDecodeCallHandler = `package containerimage

import "github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"

func extractIdentityRows(env facts.Envelope) {
	identity, err := schemadecode.DecodeContainerImageIdentity(env)
	if err != nil {
		return
	}
	_ = identity.Repository
	_ = identity.Digest
}
`

// TestScanDecodeUsageRecognizesQualifiedDecodeCall is the regression guard
// for the #6372 round-2 codex P1: recordDecodeBindings matched call.Fun only
// against *ast.Ident, so a package-qualified call
// (schemadecode.DecodeContainerImageIdentity(env), an *ast.SelectorExpr)
// returned early and every field the handler read off the decoded value
// silently disappeared from the manifest. This is not a hypothetical shape —
// it is exactly what schemadecode/AGENTS.md's "Adding a decoder" section
// instructs every family package (containerimage included) to write once a
// seam has moved into schemadecode with no root-level forwarder.
func TestScanDecodeUsageRecognizesQualifiedDecodeCall(t *testing.T) {
	t.Parallel()

	dir := writeFixtureDir(t, map[string]string{
		"extract_identity_rows.go": fixtureQualifiedDecodeCallHandler,
	})

	// FuncName is the exported subpackage spelling on purpose: a decode seam
	// called directly by its owning family (rather than through a root
	// compatibility forwarder) is parsed and, absent any ParseRootForwarders
	// entry for it, left exactly as ParseDecodeSeams found it — see
	// ResolveForwardedSeams's doc comment.
	seams := []DecodeSeam{{
		FuncName:      "DecodeContainerImageIdentity",
		FactKindConst: "FactKindContainerImageIdentity",
		StructPackage: "containerimagev1",
		StructName:    "Identity",
	}}
	usage, err := ScanDecodeUsage(dir, seams, nil)
	if err != nil {
		t.Fatalf("ScanDecodeUsage() error = %v", err)
	}

	entries := usage["DecodeContainerImageIdentity"]
	fieldNames := map[string]bool{}
	for _, e := range entries {
		fieldNames[e.GoFieldName] = true
	}

	for _, want := range []string{"Repository", "Digest"} {
		if !fieldNames[want] {
			t.Errorf("field %q read off a package-qualified decode call (schemadecode.DecodeContainerImageIdentity) was not attributed to the seam; got %+v — recordDecodeBindings likely still matches call.Fun only against *ast.Ident", want, entries)
		}
	}
}

// TestScanDecodeUsageRecognizesQualifiedCallThroughRootForwarder proves the
// second branch decodeCallName offers a qualified call: a seam that STILL has
// a root-level compatibility forwarder (var decodeAWSResource =
// schemadecode.DecodeAWSResource) must be attributed under that root name
// even when the READING call site uses the qualified/exported spelling
// (schemadecode.DecodeAWSResource(env)) instead of the unqualified one — the
// mixed-migration state where an older root call site and a newer qualified
// one coexist for the same seam. Before this test, decodeCallName's
// `forwarders[f.Sel.Name]` branch was reachable from production code
// (load.go passes a real, non-nil RootForwarders) but exercised by no test:
// every other ScanDecodeUsage test in this package passes forwarders=nil.
//
// The assertion checks the SPECIFIC root name, not merely "some attribution
// happened": the fall-through branch (`return f.Sel.Name, true`) also
// produces an attribution, just under the wrong (exported) name, so a test
// that only checked non-emptiness could not tell the two branches apart.
func TestScanDecodeUsageRecognizesQualifiedCallThroughRootForwarder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// A root-level compatibility forwarder for decodeAWSResource, the shape
	// #6061's decode_seam_compat*.go files use — this is what makes
	// ParseRootForwarders record forwarders["DecodeAWSResource"] =
	// "decodeAWSResource".
	writeFileInDir(t, dir, "decode_seam_compat.go", `package reducer

import "github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"

var decodeAWSResource = schemadecode.DecodeAWSResource
`)
	// A handler in the SAME root package that calls the seam through its
	// qualified/exported name rather than the root forwarder — the mixed
	// state this test targets.
	writeFileInDir(t, dir, "extract_rows.go", `package reducer

func extractResourceRows(env facts.Envelope) {
	resource, err := schemadecode.DecodeAWSResource(env)
	if err != nil {
		return
	}
	_ = resource.AccountID
}
`)

	forwarders, err := ParseRootForwarders(dir)
	if err != nil {
		t.Fatalf("ParseRootForwarders() error = %v", err)
	}
	if got, want := forwarders["DecodeAWSResource"], "decodeAWSResource"; got != want {
		t.Fatalf(`forwarders["DecodeAWSResource"] = %q, want %q — fixture forwarder not parsed as expected`, got, want)
	}

	// FuncName is the root spelling on purpose: this is what
	// ResolveForwardedSeams would have already rewritten DecodeSeam.FuncName
	// to, for a seam that still has a root forwarder (see its doc comment).
	seams := []DecodeSeam{{
		FuncName:      "decodeAWSResource",
		FactKindConst: "FactKindAWSResource",
		StructPackage: "awsv1",
		StructName:    "Resource",
	}}
	usage, err := ScanDecodeUsage(dir, seams, forwarders)
	if err != nil {
		t.Fatalf("ScanDecodeUsage() error = %v", err)
	}

	rootEntries := usage["decodeAWSResource"]
	found := false
	for _, e := range rootEntries {
		if e.GoFieldName == "AccountID" {
			found = true
		}
	}
	if !found {
		t.Errorf(`field "AccountID" not attributed to the root name "decodeAWSResource"; got usage["decodeAWSResource"] = %+v`, rootEntries)
	}

	if exported := usage["DecodeAWSResource"]; len(exported) != 0 {
		t.Errorf(`usage["DecodeAWSResource"] (the exported name) = %+v, want empty — the read must be attributed under the ROOT name the forwarder map resolves to, not the qualified call's own spelling`, exported)
	}
}
