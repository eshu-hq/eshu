// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package change

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseNameStatusDiffPreservesDeletedAndRenamedFiles(t *testing.T) {
	t.Parallel()

	changes := ParseNameStatusDiff("M\tgo/a.go\nD\tgo/deleted.go\nR100\tgo/old.go\tgo/new.go\n")
	if got, want := len(changes), 3; got != want {
		t.Fatalf("len(changes) = %d, want %d", got, want)
	}
	if got, want := changes[1].Status, "deleted"; got != want {
		t.Fatalf("deleted status = %q, want %q", got, want)
	}
	if got, want := changes[2].OldPath, "go/old.go"; got != want {
		t.Fatalf("renamed old path = %q, want %q", got, want)
	}
	if got, want := changes[2].Path, "go/new.go"; got != want {
		t.Fatalf("renamed path = %q, want %q", got, want)
	}
}

// TestParseNameStatusDiffSkipsShortLines covers what the two-field guard is
// for: the trailing newline every git run emits, and a status letter with no
// path behind it.
func TestParseNameStatusDiffSkipsShortLines(t *testing.T) {
	t.Parallel()

	if got := ParseNameStatusDiff(""); len(got) != 0 || got == nil {
		t.Fatalf("ParseNameStatusDiff(\"\") = %#v, want an empty non-nil slice", got)
	}
	if got := ParseNameStatusDiff("M\n\nA\tgo/added.go\n"); len(got) != 1 || got[0].Path != "go/added.go" {
		t.Fatalf("ParseNameStatusDiff() = %#v, want only the added file", got)
	}
}

// TestParseNameStatusDiffRenameWithoutTargetKeepsOnePath guards the len>=3
// condition. A rename line truncated to two fields must not leave OldPath and
// Path both naming the source file, which would tell the API a file was
// renamed onto itself.
func TestParseNameStatusDiffRenameWithoutTargetKeepsOnePath(t *testing.T) {
	t.Parallel()

	changes := ParseNameStatusDiff("R100\tgo/old.go\n")
	if len(changes) != 1 {
		t.Fatalf("len(changes) = %d, want 1", len(changes))
	}
	if changes[0].Path != "go/old.go" || changes[0].OldPath != "" {
		t.Fatalf("change = %+v, want Path=go/old.go with an empty OldPath", changes[0])
	}
}

func TestNormalizeStatus(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"A":    "added",
		"a":    "added",
		" D ":  "deleted",
		"R":    "renamed",
		"R100": "renamed",
		"C":    "copied",
		"C85":  "copied",
		"M":    "modified",
		"T":    "modified",
		"U":    "modified",
		"":     "modified",
	} {
		if got := NormalizeStatus(in); got != want {
			t.Fatalf("NormalizeStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestModifiedFilesAndChangedPaths(t *testing.T) {
	t.Parallel()

	changes := ModifiedFiles([]string{" go/a.go ", "", "   ", "go/b.go"})
	want := []FileChange{{Path: "go/a.go", Status: "modified"}, {Path: "go/b.go", Status: "modified"}}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("ModifiedFiles() = %+v, want %+v", changes, want)
	}
	if got := ChangedPaths(changes); !reflect.DeepEqual(got, []string{"go/a.go", "go/b.go"}) {
		t.Fatalf("ChangedPaths() = %#v", got)
	}
	// A rename contributes its new path only; the old one stays on the row.
	renamed := []FileChange{{OldPath: "go/old.go", Path: "go/new.go", Status: "renamed"}, {Path: "  ", Status: "modified"}}
	if got := ChangedPaths(renamed); !reflect.DeepEqual(got, []string{"go/new.go"}) {
		t.Fatalf("ChangedPaths(renamed) = %#v, want [go/new.go]", got)
	}
}

// TestCleanValuesNeverReturnsNil pins the non-nil guarantee. A nil slice
// marshals as JSON null, which the API reads as a missing field rather than as
// "no paths".
func TestCleanValuesNeverReturnsNil(t *testing.T) {
	t.Parallel()

	for _, in := range [][]string{nil, {}, {"", "   "}} {
		got := CleanValues(in)
		if got == nil {
			t.Fatalf("CleanValues(%#v) = nil, want an empty non-nil slice", in)
		}
		if len(got) != 0 {
			t.Fatalf("CleanValues(%#v) = %#v, want empty", in, got)
		}
	}
}

func TestGitDiffNameStatusDetectsCopiedFiles(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	runGit(t, repoPath, "init")
	runGit(t, repoPath, "config", "user.email", "test"+"@example.invalid")
	runGit(t, repoPath, "config", "user.name", "Test User")
	if err := os.MkdirAll(filepath.Join(repoPath, "go"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "go", "original.go"), []byte("package fixture\n\nfunc Original() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(original) error = %v", err)
	}
	runGit(t, repoPath, "add", "go/original.go")
	runGit(t, repoPath, "commit", "-m", "seed original")
	original, err := os.ReadFile(filepath.Join(repoPath, "go", "original.go"))
	if err != nil {
		t.Fatalf("ReadFile(original) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "go", "copy.go"), original, 0o644); err != nil {
		t.Fatalf("WriteFile(copy) error = %v", err)
	}
	runGit(t, repoPath, "add", "go/copy.go")

	changes, err := GitDiffNameStatus(repoPath, "HEAD", "")
	if err != nil {
		t.Fatalf("GitDiffNameStatus() error = %v", err)
	}
	if got, want := len(changes), 1; got != want {
		t.Fatalf("len(changes) = %d, want %d: %+v", got, want, changes)
	}
	if got, want := changes[0].Status, "copied"; got != want {
		t.Fatalf("copy status = %q, want %q; changes=%+v", got, want, changes)
	}
	if got, want := changes[0].OldPath, "go/original.go"; got != want {
		t.Fatalf("copy old path = %q, want %q", got, want)
	}
	if got, want := changes[0].Path, "go/copy.go"; got != want {
		t.Fatalf("copy path = %q, want %q", got, want)
	}
}

// TestGitDiffNameStatusWrapsGitFailure checks the error path, and checks what
// the message does NOT contain: the repository path an operator passed in
// --repo-path. Reporting a failure must not print where on their disk the
// repository lives.
func TestGitDiffNameStatusWrapsGitFailure(t *testing.T) {
	t.Parallel()

	repoPath := filepath.Join(t.TempDir(), "acme-payments-private-c4n4ry")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	changes, err := GitDiffNameStatus(repoPath, "HEAD", "")
	if err == nil {
		t.Fatalf("GitDiffNameStatus() on a non-repository returned %+v, want an error", changes)
	}
	if !strings.HasPrefix(err.Error(), "derive git diff: ") {
		t.Fatalf("error = %q, want a \"derive git diff: \" prefix", err.Error())
	}
	if strings.Contains(err.Error(), "c4n4ry") {
		t.Fatalf("error text leaks the operator's repository path: %q", err.Error())
	}
}

func runGit(t *testing.T, repoPath string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s error = %v output=%s", strings.Join(args, " "), err, string(out))
	}
}
