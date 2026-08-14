// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package assistantguidance

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestEngine builds an Engine rooted at a fresh temp dir backed by the real
// filesystem, which exercises the production IO path against disposable files.
func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	return NewEngine(t.TempDir())
}

func claudePlatform(t *testing.T) Platform {
	t.Helper()
	p, ok := LookupPlatform("claude")
	if !ok {
		t.Fatal("claude platform not found")
	}
	return p
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestInstallCreatesGuidanceForAllPlatforms(t *testing.T) {
	e := newTestEngine(t)
	results, err := e.Install(SupportedPlatforms())
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(results) != len(SupportedPlatforms()) {
		t.Fatalf("expected %d results, got %d", len(SupportedPlatforms()), len(results))
	}
	for _, r := range results {
		if !r.Created || !r.Changed {
			t.Fatalf("%s: expected created+changed, got %+v", r.Platform.ID, r)
		}
		if r.Status != BlockCurrent {
			t.Fatalf("%s: expected BlockCurrent, got %v", r.Platform.ID, r.Status)
		}
		content := readFile(t, r.Path)
		if !strings.Contains(content, BeginMarker) {
			t.Fatalf("%s: missing begin marker", r.Platform.ID)
		}
		// Acceptance: prefer bounded tools before raw-file search.
		if !strings.Contains(content, "before broad raw-file search") && !strings.Contains(content, "before raw-file search") {
			t.Fatalf("%s: guidance missing raw-file-search ordering", r.Platform.ID)
		}
		// Acceptance: truth-label cautions.
		for _, want := range []string{"truth.level", "truth.freshness.state", "missing"} {
			if !strings.Contains(content, want) {
				t.Fatalf("%s: guidance missing truth caution %q", r.Platform.ID, want)
			}
		}
		// Acceptance: first-prompt examples.
		if !strings.Contains(content, "First prompts") {
			t.Fatalf("%s: guidance missing first prompts", r.Platform.ID)
		}
	}
}

func TestInstallIdempotentReinstall(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.Install(SupportedPlatforms()); err != nil {
		t.Fatalf("first install: %v", err)
	}
	p := claudePlatform(t)
	path := filepath.Join(e.Root(), p.RelPath)
	first := readFile(t, path)

	results, err := e.Install([]Platform{p})
	if err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if results[0].Changed || results[0].Created {
		t.Fatalf("reinstall should be no-op, got %+v", results[0])
	}
	if got := readFile(t, path); got != first {
		t.Fatalf("reinstall changed file bytes:\nfirst=%q\ngot=%q", first, got)
	}
}

func TestInstallPreservesExistingFileContent(t *testing.T) {
	e := newTestEngine(t)
	p := claudePlatform(t)
	path := filepath.Join(e.Root(), p.RelPath)

	before := "# Team Rules\n\nAlways write tests first.\n"
	after := "## Extra Section\n\nKeep this trailing content.\n"
	original := before + "\n" + after
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	results, err := e.Install([]Platform{p})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if results[0].Created {
		t.Fatal("should not report created over an existing file")
	}
	content := readFile(t, path)
	// The seeded bytes must survive verbatim as the file's prefix, not merely
	// appear somewhere in it.
	if !strings.HasPrefix(content, strings.TrimRight(original, "\n")) {
		t.Fatalf("pre-existing content not preserved verbatim:\n want prefix=%q\n got=%q", original, content)
	}
	if !strings.Contains(content, BeginMarker) {
		t.Fatalf("guidance block not added: %q", content)
	}
}

func TestUninstallRemovesBlockKeepingOtherContent(t *testing.T) {
	e := newTestEngine(t)
	p := claudePlatform(t)
	path := filepath.Join(e.Root(), p.RelPath)

	original := "# Team Rules\n\nKeep me.\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := e.Install([]Platform{p}); err != nil {
		t.Fatalf("install: %v", err)
	}

	results, err := e.Uninstall([]Platform{p})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !results[0].Changed || results[0].Removed {
		t.Fatalf("expected changed without file removal, got %+v", results[0])
	}
	content := readFile(t, path)
	if strings.Contains(content, BeginMarker) {
		t.Fatalf("block not removed: %q", content)
	}
	// Install-then-uninstall over an existing file returns the exact seeded
	// bytes: the round trip is byte-clean, not merely content-preserving.
	if content != original {
		t.Fatalf("uninstall did not restore the seeded bytes:\n want=%q\n  got=%q", original, content)
	}
}

func TestUninstallDeletesFileEshuCreated(t *testing.T) {
	e := newTestEngine(t)
	p := claudePlatform(t)
	path := filepath.Join(e.Root(), p.RelPath)

	// Install with no pre-existing file: Eshu created it.
	if _, err := e.Install([]Platform{p}); err != nil {
		t.Fatalf("install: %v", err)
	}
	results, err := e.Uninstall([]Platform{p})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !results[0].Removed {
		t.Fatalf("expected file removal, got %+v", results[0])
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file gone, stat err=%v", err)
	}
}

func TestUninstallNoBlockIsNoOp(t *testing.T) {
	e := newTestEngine(t)
	p := claudePlatform(t)
	path := filepath.Join(e.Root(), p.RelPath)
	original := "# Just user rules\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	results, err := e.Uninstall([]Platform{p})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if results[0].Changed || results[0].Removed {
		t.Fatalf("expected no-op, got %+v", results[0])
	}
	if got := readFile(t, path); got != original {
		t.Fatalf("file modified on no-op uninstall: %q", got)
	}
}

func TestStatusReportsPerPlatformState(t *testing.T) {
	e := newTestEngine(t)
	p := claudePlatform(t)

	results, err := e.Status([]Platform{p})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if results[0].Status != BlockAbsent {
		t.Fatalf("expected absent before install, got %v", results[0].Status)
	}

	if _, err := e.Install([]Platform{p}); err != nil {
		t.Fatalf("install: %v", err)
	}
	results, err = e.Status([]Platform{p})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if results[0].Status != BlockCurrent {
		t.Fatalf("expected current after install, got %v", results[0].Status)
	}
}

func TestStatusDoesNotTouchTheFilesystem(t *testing.T) {
	e := newTestEngine(t)
	p := claudePlatform(t)
	path := filepath.Join(e.Root(), p.RelPath)
	original := "# Untouched\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	if _, err := e.Status(SupportedPlatforms()); err != nil {
		t.Fatalf("status: %v", err)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
		t.Fatal("status modified the instruction file")
	}
	if got := readFile(t, path); got != original {
		t.Fatalf("status rewrote the file: %q", got)
	}
	// The platforms status did not find must not have been created either.
	if _, err := os.Stat(filepath.Join(e.Root(), "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("status created AGENTS.md, stat err=%v", err)
	}
}

func TestSelectPlatformsUnsupportedIsError(t *testing.T) {
	if _, err := SelectPlatforms("jetbrains"); err == nil {
		t.Fatal("expected error for unsupported platform")
	}
	got, err := SelectPlatforms("")
	if err != nil {
		t.Fatalf("empty filter should succeed: %v", err)
	}
	if len(got) != len(SupportedPlatforms()) {
		t.Fatalf("empty filter should return all platforms")
	}
	one, err := SelectPlatforms("CURSOR")
	if err != nil {
		t.Fatalf("case-insensitive filter should succeed: %v", err)
	}
	if len(one) != 1 || one[0].ID != "cursor" {
		t.Fatalf("expected single cursor platform, got %+v", one)
	}
}

func TestCursorGuidanceHasFrontMatter(t *testing.T) {
	p, ok := LookupPlatform("cursor")
	if !ok {
		t.Fatal("cursor platform missing")
	}
	body := GuidanceBody(p)
	if !strings.HasPrefix(body, "---\n") || !strings.Contains(body, "alwaysApply: true") {
		t.Fatalf("cursor body missing MDC front matter: %q", body)
	}
}

func TestInstallCreatesNestedCursorDir(t *testing.T) {
	e := newTestEngine(t)
	p, ok := LookupPlatform("cursor")
	if !ok {
		t.Fatal("cursor platform missing")
	}
	results, err := e.Install([]Platform{p})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !results[0].Created {
		t.Fatal("expected cursor file created")
	}
	if _, err := os.Stat(filepath.Join(e.Root(), ".cursor", "rules", "eshu.mdc")); err != nil {
		t.Fatalf("cursor rule file not created: %v", err)
	}
}

// failingFS wraps OSFileSystem and fails one named operation, so the write and
// delete error paths run against the production Engine rather than a
// re-implementation of it.
type failingFS struct {
	OSFileSystem
	failWrite  bool
	failMkdir  bool
	failRemove bool
}

var errInjected = errors.New("injected filesystem failure")

func (f failingFS) WriteFile(path string, data []byte, perm os.FileMode) error {
	if f.failWrite {
		return errInjected
	}
	return f.OSFileSystem.WriteFile(path, data, perm)
}

func (f failingFS) MkdirAll(path string, perm os.FileMode) error {
	if f.failMkdir {
		return errInjected
	}
	return f.OSFileSystem.MkdirAll(path, perm)
}

func (f failingFS) Remove(path string) error {
	if f.failRemove {
		return errInjected
	}
	return f.OSFileSystem.Remove(path)
}

func TestEngineSurfacesFilesystemFailures(t *testing.T) {
	p := claudePlatform(t)

	t.Run("install write failure", func(t *testing.T) {
		e := NewEngineWithFS(failingFS{failWrite: true}, t.TempDir())
		_, err := e.Install([]Platform{p})
		if !errors.Is(err, errInjected) {
			t.Fatalf("install error = %v, want the injected failure", err)
		}
		if !strings.Contains(err.Error(), "write ") {
			t.Fatalf("install error lost its write context: %v", err)
		}
	})

	t.Run("install mkdir failure", func(t *testing.T) {
		e := NewEngineWithFS(failingFS{failMkdir: true}, t.TempDir())
		_, err := e.Install([]Platform{p})
		if !errors.Is(err, errInjected) {
			t.Fatalf("install error = %v, want the injected failure", err)
		}
		if !strings.Contains(err.Error(), "create dir for ") {
			t.Fatalf("install error lost its mkdir context: %v", err)
		}
	})

	t.Run("uninstall remove failure", func(t *testing.T) {
		root := t.TempDir()
		if _, err := NewEngine(root).Install([]Platform{p}); err != nil {
			t.Fatalf("seed install: %v", err)
		}
		e := NewEngineWithFS(failingFS{failRemove: true}, root)
		_, err := e.Uninstall([]Platform{p})
		if !errors.Is(err, errInjected) {
			t.Fatalf("uninstall error = %v, want the injected failure", err)
		}
		if !strings.Contains(err.Error(), "remove ") {
			t.Fatalf("uninstall error lost its remove context: %v", err)
		}
	})
}

// TestReadFileOrEmptyTreatsMissingAsAbsent guards the os.IsNotExist check that
// the OSFileSystem nolint directives exist to protect: a missing file is not an
// error, and a %w wrap inside OSFileSystem would break that (os.IsNotExist does
// not unwrap).
func TestReadFileOrEmptyTreatsMissingAsAbsent(t *testing.T) {
	e := newTestEngine(t)
	content, existed, err := e.readFileOrEmpty(filepath.Join(e.Root(), "nope.md"))
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if existed || content != "" {
		t.Fatalf("missing file reported as existing: existed=%v content=%q", existed, content)
	}
}
