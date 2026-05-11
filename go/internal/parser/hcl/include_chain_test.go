package hcl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestResolveTerragruntRemoteStateFromSelf(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	selfPath := filepath.Join(root, "terragrunt.hcl")
	if err := os.WriteFile(selfPath, []byte(`remote_state {
  backend = "s3"
  config = {
    bucket = "self-bucket"
    key    = "self.tfstate"
    region = "us-east-1"
  }
}
`), 0o644); err != nil {
		t.Fatalf("write self error = %v", err)
	}

	resolved, warnings := resolveTerragruntRemoteState(selfPath)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	if resolved == nil {
		t.Fatal("resolved = nil, want non-nil")
	}
	if got, want := resolved.row["backend_kind"], "s3"; got != want {
		t.Fatalf("backend_kind = %#v, want %#v", got, want)
	}
	if got, want := resolved.resolvedFrom, "self"; got != want {
		t.Fatalf("resolvedFrom = %#v, want %#v", got, want)
	}
}

func TestResolveTerragruntRemoteStateWalksParentInclude(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	parentDir := filepath.Join(root, "live")
	childDir := filepath.Join(parentDir, "prod", "api")
	if err := os.MkdirAll(childDir, 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(parentDir, "terragrunt.hcl"), []byte(`remote_state {
  backend = "s3"
  config = {
    bucket = "parent-bucket"
    key    = "parent.tfstate"
    region = "us-east-1"
  }
}
`), 0o644); err != nil {
		t.Fatalf("write parent error = %v", err)
	}

	childPath := filepath.Join(childDir, "terragrunt.hcl")
	if err := os.WriteFile(childPath, []byte(`include "root" {
  path = find_in_parent_folders("terragrunt.hcl")
}
`), 0o644); err != nil {
		t.Fatalf("write child error = %v", err)
	}

	resolved, warnings := resolveTerragruntRemoteState(childPath)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	if resolved == nil {
		t.Fatal("resolved = nil, want non-nil")
	}
	if got, want := resolved.row["bucket"], "parent-bucket"; got != want {
		t.Fatalf("bucket = %#v, want %#v", got, want)
	}
	if got, want := resolved.resolvedFrom, "include_chain"; got != want {
		t.Fatalf("resolvedFrom = %#v, want %#v", got, want)
	}
}

func TestResolveTerragruntRemoteStateAbsoluteIncludePath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	parentPath := filepath.Join(root, "shared.hcl")
	if err := os.WriteFile(parentPath, []byte(`remote_state {
  backend = "s3"
  config = {
    bucket = "abs-bucket"
    key    = "abs.tfstate"
    region = "us-east-1"
  }
}
`), 0o644); err != nil {
		t.Fatalf("write shared error = %v", err)
	}

	childPath := filepath.Join(root, "terragrunt.hcl")
	body := "include \"root\" {\n  path = \"" + parentPath + "\"\n}\n"
	if err := os.WriteFile(childPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write child error = %v", err)
	}

	resolved, warnings := resolveTerragruntRemoteState(childPath)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	if resolved == nil {
		t.Fatal("resolved = nil, want non-nil")
	}
	if got, want := resolved.row["bucket"], "abs-bucket"; got != want {
		t.Fatalf("bucket = %#v, want %#v", got, want)
	}
}

func TestResolveTerragruntRemoteStateMissingParentNoCrash(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	childPath := filepath.Join(root, "terragrunt.hcl")
	if err := os.WriteFile(childPath, []byte(`include "root" {
  path = find_in_parent_folders("does-not-exist.hcl")
}
`), 0o644); err != nil {
		t.Fatalf("write child error = %v", err)
	}

	resolved, warnings := resolveTerragruntRemoteState(childPath)
	if resolved != nil {
		t.Fatalf("resolved = %#v, want nil for missing parent", resolved)
	}
	// Missing parent is not an error worth a warning row; absence is signal enough.
	_ = warnings
}

func TestResolveTerragruntRemoteStateDepthLimit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	const depth = 20
	leafDir := root
	for i := 0; i < depth; i++ {
		leafDir = filepath.Join(leafDir, "level")
	}
	if err := os.MkdirAll(leafDir, 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	// Each level except the leaf includes its parent folder's terragrunt.hcl.
	curr := leafDir
	for i := 0; i < depth; i++ {
		body := `include "p" {
  path = find_in_parent_folders("terragrunt.hcl")
}
`
		if err := os.WriteFile(filepath.Join(curr, "terragrunt.hcl"), []byte(body), 0o644); err != nil {
			t.Fatalf("write level error = %v", err)
		}
		curr = filepath.Dir(curr)
	}

	leafPath := filepath.Join(leafDir, "terragrunt.hcl")
	resolved, warnings := resolveTerragruntRemoteState(leafPath)
	if resolved != nil {
		t.Fatalf("resolved = %#v, want nil for too-deep chain", resolved)
	}
	if !containsWarningKind(warnings, "terragrunt_include_depth_exceeded") {
		t.Fatalf("warnings = %#v, want one with terragrunt_include_depth_exceeded", warnings)
	}
}

func TestResolveTerragruntRemoteStateCycleDetected(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	a := filepath.Join(root, "a.hcl")
	b := filepath.Join(root, "b.hcl")
	if err := os.WriteFile(a, []byte("include \"x\" {\n  path = \""+b+"\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write a error = %v", err)
	}
	if err := os.WriteFile(b, []byte("include \"x\" {\n  path = \""+a+"\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write b error = %v", err)
	}

	resolved, warnings := resolveTerragruntRemoteState(a)
	if resolved != nil {
		t.Fatalf("resolved = %#v, want nil for cycle", resolved)
	}
	if !containsWarningKind(warnings, "terragrunt_include_cycle") {
		t.Fatalf("warnings = %#v, want one with terragrunt_include_cycle", warnings)
	}
}

func containsWarningKind(warnings []terragruntIncludeWarning, kind string) bool {
	for _, w := range warnings {
		if strings.EqualFold(w.kind, kind) {
			return true
		}
	}
	return false
}

// TestResolveTerragruntIncludeWarningsSurfaceInPayload guarantees that a
// depth-exceeded include chain emits a row in the parser's
// terragrunt_include_warnings payload bucket. Without this surfacing the
// walker's bound is invisible to downstream consumers, which conflicts with
// the PR description's claim that include-chain failures are observable.
func TestResolveTerragruntIncludeWarningsSurfaceInPayload(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	const depth = 20
	leafDir := root
	for i := 0; i < depth; i++ {
		leafDir = filepath.Join(leafDir, "level")
	}
	if err := os.MkdirAll(leafDir, 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	curr := leafDir
	for i := 0; i < depth; i++ {
		body := `include "p" {
  path = find_in_parent_folders("terragrunt.hcl")
}
`
		if err := os.WriteFile(filepath.Join(curr, "terragrunt.hcl"), []byte(body), 0o644); err != nil {
			t.Fatalf("write level error = %v", err)
		}
		curr = filepath.Dir(curr)
	}

	leafPath := filepath.Join(leafDir, "terragrunt.hcl")
	got, err := Parse(leafPath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	warnings := bucketForTest(t, got, "terragrunt_include_warnings")
	if len(warnings) == 0 {
		t.Fatalf("len(terragrunt_include_warnings) = 0, want at least 1")
	}
	found := false
	for _, w := range warnings {
		if kind, _ := w["kind"].(string); kind == "terragrunt_include_depth_exceeded" {
			if reason, _ := w["reason"].(string); reason == "" {
				t.Fatalf("warning row has empty reason: %#v", w)
			}
			if source, _ := w["source"].(string); source == "" {
				t.Fatalf("warning row has empty source: %#v", w)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("warnings = %#v, want one with kind=terragrunt_include_depth_exceeded", warnings)
	}
}

// TestResolveTerragruntRemoteStateRejectsOversizeIncludeFile ensures the
// walker bounds the bytes it reads from disk so a malicious include cannot
// trigger an unbounded os.ReadFile against /dev/zero, /proc/kcore, or a
// large attacker-controlled file dropped in the repo.
func TestResolveTerragruntRemoteStateRejectsOversizeIncludeFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	parentPath := filepath.Join(root, "shared.hcl")
	// Write a file larger than the parser cap. The contents are inert HCL
	// padding; the size check fires before the parser ever sees the bytes.
	header := []byte("# padding\n")
	padding := make([]byte, terragruntIncludeMaxFileBytes+1024)
	for i := range padding {
		padding[i] = '\n'
	}
	if err := os.WriteFile(parentPath, append(header, padding...), 0o644); err != nil {
		t.Fatalf("write oversize parent error = %v", err)
	}

	childPath := filepath.Join(root, "terragrunt.hcl")
	body := "include \"root\" {\n  path = \"" + parentPath + "\"\n}\n"
	if err := os.WriteFile(childPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write child error = %v", err)
	}

	resolved, warnings := resolveTerragruntRemoteState(childPath)
	if resolved != nil {
		t.Fatalf("resolved = %#v, want nil for oversize include target", resolved)
	}
	if !containsWarningKind(warnings, "terragrunt_include_unsafe_file") {
		t.Fatalf("warnings = %#v, want one with terragrunt_include_unsafe_file", warnings)
	}
}

// TestResolveTerragruntRemoteStateRejectsIrregularIncludeFile ensures the
// walker refuses to read non-regular files reached through include chains so
// special device files (/dev/*, /proc/*) or symlinked attacker targets cannot
// be slurped into the parser.
func TestResolveTerragruntRemoteStateRejectsIrregularIncludeFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "real.hcl")
	if err := os.WriteFile(target, []byte(`remote_state {
  backend = "s3"
  config = {
    bucket = "abs-bucket"
    key    = "abs.tfstate"
    region = "us-east-1"
  }
}
`), 0o644); err != nil {
		t.Fatalf("write target error = %v", err)
	}
	symlink := filepath.Join(root, "shared.hcl")
	if err := os.Symlink(target, symlink); err != nil {
		t.Skipf("symlink not supported on this platform: %v", err)
	}

	childPath := filepath.Join(root, "terragrunt.hcl")
	body := "include \"root\" {\n  path = \"" + symlink + "\"\n}\n"
	if err := os.WriteFile(childPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write child error = %v", err)
	}

	resolved, warnings := resolveTerragruntRemoteState(childPath)
	if resolved != nil {
		t.Fatalf("resolved = %#v, want nil for symlinked include target", resolved)
	}
	if !containsWarningKind(warnings, "terragrunt_include_unsafe_file") {
		t.Fatalf("warnings = %#v, want one with terragrunt_include_unsafe_file", warnings)
	}
}
