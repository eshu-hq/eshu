// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hcl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// classicMacHCL renders lines with a bare carriage return as the only
// terminator -- the classic-Mac form that carries no '\n' anywhere.
func classicMacHCL(lines ...string) []byte {
	return []byte(strings.Join(lines, "\r") + "\r")
}

// terragruntRemoteStateLines is a commented remote_state block. The leading
// comment is the load-bearing part: hclsyntax terminates a `#` or `//`
// comment at its own newline set, which is `\n` and `\r\n` only (vendored
// hcl/v2 hclsyntax/scan_tokens.rl: `Newline = '\r' ? '\n'`). With no '\n' in
// the file, the comment therefore runs to EOF and swallows every block after
// it -- silently, since the parse itself reports no error.
var terragruntRemoteStateLines = []string{
	"# managed by the platform team",
	"# do not edit by hand",
	"remote_state {",
	`  backend = "s3"`,
	"  config = {",
	`    bucket = "parent-bucket"`,
	`    key    = "parent.tfstate"`,
	`    region = "us-east-1"`,
	"  }",
	"}",
}

// TestResolveTerragruntRemoteStateReadsClassicMacIncludeFile is the
// regression for the include-chain read that bypasses shared.ReadSource.
// walkTerragruntIncludeChain does its own os.ReadFile and hands the bytes
// straight to hclparse.ParseHCL, so it inherits none of the read-boundary
// normalization (issue #6306). On a classic-Mac parent the leading comment
// eats the rest of the file and every remote_state block disappears with no
// warning -- the same silent-loss class the read boundary exists to close.
func TestResolveTerragruntRemoteStateReadsClassicMacIncludeFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	parentDir := filepath.Join(root, "live")
	childDir := filepath.Join(parentDir, "prod", "api")
	if err := os.MkdirAll(childDir, 0o750); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v, want nil", childDir, err)
	}

	parentPath := filepath.Join(parentDir, "terragrunt.hcl")
	if err := os.WriteFile(parentPath, classicMacHCL(terragruntRemoteStateLines...), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", parentPath, err)
	}

	childPath := filepath.Join(childDir, "terragrunt.hcl")
	child := "include \"root\" {\n  path = find_in_parent_folders(\"terragrunt.hcl\")\n}\n"
	if err := os.WriteFile(childPath, []byte(child), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", childPath, err)
	}

	resolved, warnings := resolveTerragruntRemoteState(childPath)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	if resolved == nil {
		t.Fatal("resolved = nil, want the parent's remote_state; a bare-CR include file must be normalized before hclparse sees it")
	}
	if got, want := resolved.row["bucket"], "parent-bucket"; got != want {
		t.Fatalf("bucket = %#v, want %#v", got, want)
	}
	if got, want := resolved.resolvedFrom, "include_chain"; got != want {
		t.Fatalf("resolvedFrom = %#v, want %#v", got, want)
	}
	if got, want := resolved.row["line_number"], 3; got != want {
		t.Fatalf("line_number = %#v, want %#v (the physical line of the remote_state block)", got, want)
	}
}

// TestResolveTerragruntRemoteStateReadsClassicMacStartFile covers the same
// read for the file the walk STARTS on. walkTerragruntIncludeChain reads the
// start path with the identical os.ReadFile, so a self-declared remote_state
// in a classic-Mac terragrunt.hcl is lost the same way a parent's is.
func TestResolveTerragruntRemoteStateReadsClassicMacStartFile(t *testing.T) {
	t.Parallel()

	selfPath := filepath.Join(t.TempDir(), "terragrunt.hcl")
	if err := os.WriteFile(selfPath, classicMacHCL(terragruntRemoteStateLines...), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", selfPath, err)
	}

	resolved, warnings := resolveTerragruntRemoteState(selfPath)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	if resolved == nil {
		t.Fatal("resolved = nil, want the file's own remote_state")
	}
	if got, want := resolved.row["backend_kind"], "s3"; got != want {
		t.Fatalf("backend_kind = %#v, want %#v", got, want)
	}
	if got, want := resolved.resolvedFrom, "self"; got != want {
		t.Fatalf("resolvedFrom = %#v, want %#v", got, want)
	}
	if got, want := resolved.row["line_number"], 3; got != want {
		t.Fatalf("line_number = %#v, want %#v (the physical line of the remote_state block)", got, want)
	}
}

// TestResolveTerragruntRemoteStateFollowsClassicMacIncludeDeclaration is a
// characterization test, NOT a regression test: it passed before the
// normalization landed and passes after. It is recorded because the reason it
// passed is easy to mistake for the AST working. It does not.
// collectTerragruntIncludeTargets finds nothing in the parsed body -- the
// leading comment swallowed the include block -- and the target is recovered
// only by its raw-source regex fallback (collectNormalizedHelperPaths over
// terragruntFindInParentFoldersPattern), which never looks at line endings.
// So the include DECLARATION survived a bare-CR child by accident while the
// remote_state BLOCK, which has no text fallback, was lost outright.
//
// Its value now is the other direction: normalization must not break the
// fallback path that was carrying this case.
func TestResolveTerragruntRemoteStateFollowsClassicMacIncludeDeclaration(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	parentDir := filepath.Join(root, "live")
	childDir := filepath.Join(parentDir, "prod", "api")
	if err := os.MkdirAll(childDir, 0o750); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v, want nil", childDir, err)
	}

	parent := "remote_state {\n  backend = \"s3\"\n  config = {\n    bucket = \"parent-bucket\"\n  }\n}\n"
	parentPath := filepath.Join(parentDir, "terragrunt.hcl")
	if err := os.WriteFile(parentPath, []byte(parent), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", parentPath, err)
	}

	childPath := filepath.Join(childDir, "terragrunt.hcl")
	child := classicMacHCL(
		"# leaf stack",
		`include "root" {`,
		`  path = find_in_parent_folders("terragrunt.hcl")`,
		"}",
	)
	if err := os.WriteFile(childPath, child, 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", childPath, err)
	}

	resolved, warnings := resolveTerragruntRemoteState(childPath)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	if resolved == nil {
		t.Fatal("resolved = nil, want the parent's remote_state reached through a bare-CR include declaration")
	}
	if got, want := resolved.row["bucket"], "parent-bucket"; got != want {
		t.Fatalf("bucket = %#v, want %#v", got, want)
	}
}
