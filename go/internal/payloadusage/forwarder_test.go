// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFileInDir writes contents to rel (a path relative to dir, which may
// include subdirectories) and returns the absolute path written. Unlike
// writeFixtureFile (which always creates its own fresh TempDir), this lets a
// test place several files under one shared root — required to prove
// ParseRootForwarders reads only that root's own files, not a subdirectory's.
func writeFileInDir(t *testing.T, dir, rel, contents string) string {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
	return path
}

// TestParseRootForwardersVarBinding proves a root-level var forwarder
// (`var decodeAWSResource = schemadecode.DecodeAWSResource`, the shape
// #6061's compatibility files use) is recorded as a mapping from the
// relocated package's exported target name back to the root's lowercase
// name — the exact value the resolution must produce, not merely "no error".
func TestParseRootForwardersVarBinding(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFileInDir(t, dir, "decode_seam_compat.go", `package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
)

var (
	decodeAWSResource     = schemadecode.DecodeAWSResource
	decodeAWSIAMPrincipal = schemadecode.DecodeAWSIAMPrincipal
)
`)

	forwarders, err := ParseRootForwarders(dir)
	if err != nil {
		t.Fatalf("ParseRootForwarders() error = %v", err)
	}

	if got, want := forwarders["DecodeAWSResource"], "decodeAWSResource"; got != want {
		t.Errorf(`forwarders["DecodeAWSResource"] = %q, want %q`, got, want)
	}
	if got, want := forwarders["DecodeAWSIAMPrincipal"], "decodeAWSIAMPrincipal"; got != want {
		t.Errorf(`forwarders["DecodeAWSIAMPrincipal"] = %q, want %q`, got, want)
	}
	if len(forwarders) != 2 {
		t.Errorf("len(forwarders) = %d, want 2: %+v", len(forwarders), forwarders)
	}
}

// TestParseRootForwardersFuncBinding proves a root-level func forwarder
// (`func decodeX(env facts.Envelope) (T, error) { return
// schemadecode.DecodeX(env) }`, the shape factschemaEnvelope uses in
// decode_seam_compat3.go) is recorded the same way a var forwarder is.
func TestParseRootForwardersFuncBinding(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFileInDir(t, dir, "decode_seam_compat3.go", `package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"

	factschema "github.com/eshu-hq/eshu/sdk/go/factschema"
)

func factschemaEnvelope(env facts.Envelope) factschema.Envelope {
	return schemadecode.FactschemaEnvelope(env)
}
`)

	forwarders, err := ParseRootForwarders(dir)
	if err != nil {
		t.Fatalf("ParseRootForwarders() error = %v", err)
	}

	if got, want := forwarders["FactschemaEnvelope"], "factschemaEnvelope"; got != want {
		t.Errorf(`forwarders["FactschemaEnvelope"] = %q, want %q`, got, want)
	}
}

// TestParseRootForwardersIgnoresLocalTargets proves a var binding whose RHS
// is a bare local identifier (not a package-qualified selector) is NOT
// treated as a relocation forwarder — it is an ordinary root-local var and
// must not pollute the resolution map.
func TestParseRootForwardersIgnoresLocalTargets(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFileInDir(t, dir, "local_vars.go", `package reducer

func localDecodeHelper() {}

var decodeLocal = localDecodeHelper
`)

	forwarders, err := ParseRootForwarders(dir)
	if err != nil {
		t.Fatalf("ParseRootForwarders() error = %v", err)
	}
	if len(forwarders) != 0 {
		t.Errorf("forwarders = %+v, want empty (bare-identifier RHS is not a relocation forwarder)", forwarders)
	}
}

// TestParseRootForwardersIgnoresLocalFuncBody proves a func whose body
// returns a call to a LOCAL (unqualified) function is not mistaken for a
// relocation forwarder, since its target is not a package-qualified name.
func TestParseRootForwardersIgnoresLocalFuncBody(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFileInDir(t, dir, "local_func.go", `package reducer

func localHelper() int { return 1 }

func decodeY() int {
	return localHelper()
}
`)

	forwarders, err := ParseRootForwarders(dir)
	if err != nil {
		t.Fatalf("ParseRootForwarders() error = %v", err)
	}
	if len(forwarders) != 0 {
		t.Errorf("forwarders = %+v, want empty (local-function body is not a relocation forwarder)", forwarders)
	}
}

// TestParseRootForwardersNonRecursive proves ParseRootForwarders reads only
// dir's own files, never a subdirectory's — the reducer root forwarders
// compatibility surface lives directly in go/internal/reducer, and the
// relocated seams themselves live one level down in schemadecode/; scanning
// recursively would find the relocated seam declarations too and corrupt the
// mapping.
func TestParseRootForwardersNonRecursive(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFileInDir(t, dir, "sub/decode_seam_compat.go", `package sub

var decodeShouldNotBeFound = other.DecodeShouldNotBeFound
`)

	forwarders, err := ParseRootForwarders(dir)
	if err != nil {
		t.Fatalf("ParseRootForwarders() error = %v", err)
	}
	if len(forwarders) != 0 {
		t.Errorf("forwarders = %+v, want empty (subdirectory files must not be scanned)", forwarders)
	}
}

// TestParseRootForwardersEmptyWhenNone proves a directory with no forwarder
// files at all (the pre-#6061 world, and today's projector/query dirs)
// yields an empty map with no error, so callers can apply resolution
// unconditionally without special-casing "no forwarders exist".
func TestParseRootForwardersEmptyWhenNone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFileInDir(t, dir, "plain.go", `package reducer

func decodeAWSResource() {}
`)

	forwarders, err := ParseRootForwarders(dir)
	if err != nil {
		t.Fatalf("ParseRootForwarders() error = %v", err)
	}
	if len(forwarders) != 0 {
		t.Errorf("forwarders = %+v, want empty", forwarders)
	}
}

// TestParseRootForwardersSkipsTestFiles proves an _test.go file's own
// var/func declarations (test helpers, table fixtures) are never scanned as
// forwarders.
func TestParseRootForwardersSkipsTestFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFileInDir(t, dir, "decode_seam_compat_test.go", `package reducer

var decodeTestOnly = schemadecode.DecodeTestOnly
`)

	forwarders, err := ParseRootForwarders(dir)
	if err != nil {
		t.Fatalf("ParseRootForwarders() error = %v", err)
	}
	if len(forwarders) != 0 {
		t.Errorf("forwarders = %+v, want empty (_test.go files must not be scanned)", forwarders)
	}
}

// TestResolveForwardedSeams proves ResolveForwardedSeams rewrites a seam's
// FuncName from the relocated package's exported target name back to the
// root's forwarder name when one exists, and leaves a seam with no matching
// forwarder untouched.
func TestResolveForwardedSeams(t *testing.T) {
	t.Parallel()

	seams := []DecodeSeam{
		{FuncName: "DecodeAWSResource", FactKindConst: "FactKindAWSResource", StructPackage: "awsv1", StructName: "Resource"},
		{FuncName: "decodeUnforwarded", FactKindConst: "FactKindOther", StructPackage: "otherv1", StructName: "Other"},
	}
	forwarders := RootForwarders{"DecodeAWSResource": "decodeAWSResource"}

	resolved := ResolveForwardedSeams(seams, forwarders)

	byFactKind := map[string]DecodeSeam{}
	for _, s := range resolved {
		byFactKind[s.FactKindConst] = s
	}

	if got, want := byFactKind["FactKindAWSResource"].FuncName, "decodeAWSResource"; got != want {
		t.Errorf("resolved FactKindAWSResource FuncName = %q, want %q", got, want)
	}
	if got, want := byFactKind["FactKindOther"].FuncName, "decodeUnforwarded"; got != want {
		t.Errorf("resolved FactKindOther FuncName = %q, want %q (unforwarded seam must stay unchanged)", got, want)
	}
}

// TestResolveForwardedSeamsEmptyForwarders proves an empty forwarder map
// (the no-forwarders-exist case) leaves every seam's FuncName completely
// unchanged, matching pre-#6061 behavior exactly.
func TestResolveForwardedSeamsEmptyForwarders(t *testing.T) {
	t.Parallel()

	seams := []DecodeSeam{
		{FuncName: "decodeAWSResource", FactKindConst: "FactKindAWSResource", StructPackage: "awsv1", StructName: "Resource"},
	}

	resolved := ResolveForwardedSeams(seams, RootForwarders{})

	if len(resolved) != 1 || resolved[0].FuncName != "decodeAWSResource" {
		t.Errorf("resolved = %+v, want seams unchanged", resolved)
	}
}
