// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newTestEngine builds a guidanceEngine rooted at a fresh temp dir backed by the
// real filesystem, which exercises the production IO path against disposable
// files.
func newTestEngine(t *testing.T) *guidanceEngine {
	t.Helper()
	return &guidanceEngine{fs: osFileSystem{}, root: t.TempDir()}
}

func claudePlatform(t *testing.T) assistantPlatform {
	t.Helper()
	p, ok := lookupPlatform("claude")
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
	results, err := e.install(supportedPlatforms())
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(results) != len(supportedPlatforms()) {
		t.Fatalf("expected %d results, got %d", len(supportedPlatforms()), len(results))
	}
	for _, r := range results {
		if !r.created || !r.changed {
			t.Fatalf("%s: expected created+changed, got %+v", r.platform.id, r)
		}
		if r.status != blockCurrent {
			t.Fatalf("%s: expected blockCurrent, got %v", r.platform.id, r.status)
		}
		content := readFile(t, r.path)
		if !strings.Contains(content, guidanceBeginMarker) {
			t.Fatalf("%s: missing begin marker", r.platform.id)
		}
		// Acceptance: prefer bounded tools before raw-file search.
		if !strings.Contains(content, "before broad raw-file search") && !strings.Contains(content, "before raw-file search") {
			t.Fatalf("%s: guidance missing raw-file-search ordering", r.platform.id)
		}
		// Acceptance: truth-label cautions.
		for _, want := range []string{"truth.level", "truth.freshness.state", "missing"} {
			if !strings.Contains(content, want) {
				t.Fatalf("%s: guidance missing truth caution %q", r.platform.id, want)
			}
		}
		// Acceptance: first-prompt examples.
		if !strings.Contains(content, "First prompts") {
			t.Fatalf("%s: guidance missing first prompts", r.platform.id)
		}
	}
}

func TestInstallIdempotentReinstall(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.install(supportedPlatforms()); err != nil {
		t.Fatalf("first install: %v", err)
	}
	p := claudePlatform(t)
	path := filepath.Join(e.root, p.relPath)
	first := readFile(t, path)

	results, err := e.install([]assistantPlatform{p})
	if err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if results[0].changed || results[0].created {
		t.Fatalf("reinstall should be no-op, got %+v", results[0])
	}
	if got := readFile(t, path); got != first {
		t.Fatalf("reinstall changed file bytes:\nfirst=%q\ngot=%q", first, got)
	}
}

func TestInstallPreservesExistingFileContent(t *testing.T) {
	e := newTestEngine(t)
	p := claudePlatform(t)
	path := filepath.Join(e.root, p.relPath)

	before := "# Team Rules\n\nAlways write tests first.\n"
	after := "## Extra Section\n\nKeep this trailing content.\n"
	original := before + "\n" + after
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	results, err := e.install([]assistantPlatform{p})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if results[0].created {
		t.Fatal("should not report created over an existing file")
	}
	content := readFile(t, path)
	if !strings.Contains(content, "Always write tests first.") {
		t.Fatalf("pre-existing head content lost: %q", content)
	}
	if !strings.Contains(content, "Keep this trailing content.") {
		t.Fatalf("pre-existing trailing content lost: %q", content)
	}
	if !strings.Contains(content, guidanceBeginMarker) {
		t.Fatalf("guidance block not added: %q", content)
	}
}

func TestUninstallRemovesBlockKeepingOtherContent(t *testing.T) {
	e := newTestEngine(t)
	p := claudePlatform(t)
	path := filepath.Join(e.root, p.relPath)

	original := "# Team Rules\n\nKeep me.\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := e.install([]assistantPlatform{p}); err != nil {
		t.Fatalf("install: %v", err)
	}

	results, err := e.uninstall([]assistantPlatform{p})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !results[0].changed || results[0].removed {
		t.Fatalf("expected changed without file removal, got %+v", results[0])
	}
	content := readFile(t, path)
	if strings.Contains(content, guidanceBeginMarker) {
		t.Fatalf("block not removed: %q", content)
	}
	if !strings.Contains(content, "Keep me.") {
		t.Fatalf("user content lost on uninstall: %q", content)
	}
}

func TestUninstallDeletesFileEshuCreated(t *testing.T) {
	e := newTestEngine(t)
	p := claudePlatform(t)
	path := filepath.Join(e.root, p.relPath)

	// Install with no pre-existing file: Eshu created it.
	if _, err := e.install([]assistantPlatform{p}); err != nil {
		t.Fatalf("install: %v", err)
	}
	results, err := e.uninstall([]assistantPlatform{p})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !results[0].removed {
		t.Fatalf("expected file removal, got %+v", results[0])
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file gone, stat err=%v", err)
	}
}

func TestUninstallNoBlockIsNoOp(t *testing.T) {
	e := newTestEngine(t)
	p := claudePlatform(t)
	path := filepath.Join(e.root, p.relPath)
	original := "# Just user rules\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	results, err := e.uninstall([]assistantPlatform{p})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if results[0].changed || results[0].removed {
		t.Fatalf("expected no-op, got %+v", results[0])
	}
	if got := readFile(t, path); got != original {
		t.Fatalf("file modified on no-op uninstall: %q", got)
	}
}

func TestStatusReportsPerPlatformState(t *testing.T) {
	e := newTestEngine(t)
	p := claudePlatform(t)

	results, err := e.status([]assistantPlatform{p})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if results[0].status != blockAbsent {
		t.Fatalf("expected absent before install, got %v", results[0].status)
	}

	if _, err := e.install([]assistantPlatform{p}); err != nil {
		t.Fatalf("install: %v", err)
	}
	results, err = e.status([]assistantPlatform{p})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if results[0].status != blockCurrent {
		t.Fatalf("expected current after install, got %v", results[0].status)
	}
}

func TestAssistantStatusDefaultOmitsVerification(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.install(supportedPlatforms()); err != nil {
		t.Fatalf("install: %v", err)
	}
	results, err := e.status(supportedPlatforms())
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	out := captureStdout(t, func() {
		if err := renderAssistantStatus(e.root, results, false); err != nil {
			t.Fatalf("renderAssistantStatus() error = %v", err)
		}
	})
	if strings.Contains(out, "Assistant ritual verification") {
		t.Fatalf("default status should not print verification block:\n%s", out)
	}
	if !strings.Contains(out, "Claude Code") || !strings.Contains(out, "current") {
		t.Fatalf("default status missing platform table:\n%s", out)
	}
}

func TestAssistantStatusVerifyReportsLocalStdioDiagnostics(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.install(supportedPlatforms()); err != nil {
		t.Fatalf("install: %v", err)
	}
	results, err := e.status(supportedPlatforms())
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	out := captureStdout(t, func() {
		if err := renderAssistantStatus(e.root, results, true); err != nil {
			t.Fatalf("renderAssistantStatus(--verify) error = %v", err)
		}
	})
	for _, want := range []string{
		"Assistant ritual verification",
		"[ok] guidance installed",
		"3/3 platform guidance blocks current",
		"config generated",
		"tools visible",
		"no endpoint to probe (local stdio)",
		"no endpoint to query (local stdio)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("verify output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, fakeBearerToken) {
		t.Fatalf("verify output leaked token:\n%s", out)
	}
}

func TestAssistantStatusVerifyFailsWhenGuidanceMissing(t *testing.T) {
	e := newTestEngine(t)
	p := claudePlatform(t)
	results, err := e.status([]assistantPlatform{p})
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	out := captureStdout(t, func() {
		if err := renderAssistantStatus(e.root, results, true); err == nil {
			t.Fatal("renderAssistantStatus(--verify) error = nil, want failure")
		}
	})
	if !strings.Contains(out, "[!!] guidance installed") {
		t.Fatalf("missing guidance should fail verification:\n%s", out)
	}
	if !strings.Contains(out, "0/1 platform guidance blocks current") {
		t.Fatalf("missing guidance count not reported:\n%s", out)
	}
}

func TestAssistantStatusCommandHasVerifyFlag(t *testing.T) {
	var statusCmd *cobra.Command
	for _, cmd := range assistantCmd.Commands() {
		if cmd.Name() == "status" {
			statusCmd = cmd
			break
		}
	}
	if statusCmd == nil {
		t.Fatal("assistant status command not registered")
	}
	if flag := statusCmd.Flags().Lookup("verify"); flag == nil {
		t.Fatal("assistant status command missing --verify flag")
	}
}

func TestSelectPlatformsUnsupportedIsError(t *testing.T) {
	if _, err := selectPlatforms("jetbrains"); err == nil {
		t.Fatal("expected error for unsupported platform")
	}
	got, err := selectPlatforms("")
	if err != nil {
		t.Fatalf("empty filter should succeed: %v", err)
	}
	if len(got) != len(supportedPlatforms()) {
		t.Fatalf("empty filter should return all platforms")
	}
	one, err := selectPlatforms("CURSOR")
	if err != nil {
		t.Fatalf("case-insensitive filter should succeed: %v", err)
	}
	if len(one) != 1 || one[0].id != "cursor" {
		t.Fatalf("expected single cursor platform, got %+v", one)
	}
}

func TestCursorGuidanceHasFrontMatter(t *testing.T) {
	p, ok := lookupPlatform("cursor")
	if !ok {
		t.Fatal("cursor platform missing")
	}
	body := guidanceBody(p)
	if !strings.HasPrefix(body, "---\n") || !strings.Contains(body, "alwaysApply: true") {
		t.Fatalf("cursor body missing MDC front matter: %q", body)
	}
}

func TestInstallCreatesNestedCursorDir(t *testing.T) {
	e := newTestEngine(t)
	p, _ := lookupPlatform("cursor")
	results, err := e.install([]assistantPlatform{p})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !results[0].created {
		t.Fatal("expected cursor file created")
	}
	if _, err := os.Stat(filepath.Join(e.root, ".cursor", "rules", "eshu.mdc")); err != nil {
		t.Fatalf("cursor rule file not created: %v", err)
	}
}

func TestAssistantInstallDefaultOmitsVerification(t *testing.T) {
	e := newTestEngine(t)
	results, err := e.install(supportedPlatforms())
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	out := captureStdout(t, func() {
		if err := renderAssistantInstall(e.root, results, false); err != nil {
			t.Fatalf("renderAssistantInstall() error = %v", err)
		}
	})
	if strings.Contains(out, "Assistant ritual verification") {
		t.Fatalf("default install should not print verification block:\n%s", out)
	}
	if !strings.Contains(out, "created CLAUDE.md with Eshu guidance") {
		t.Fatalf("default install missing install result:\n%s", out)
	}
}

func TestAssistantInstallVerifyReportsLocalStdioDiagnostics(t *testing.T) {
	e := newTestEngine(t)
	results, err := e.install(supportedPlatforms())
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	out := captureStdout(t, func() {
		if err := renderAssistantInstall(e.root, results, true); err != nil {
			t.Fatalf("renderAssistantInstall(--verify) error = %v", err)
		}
	})
	for _, want := range []string{
		"created CLAUDE.md with Eshu guidance",
		"Assistant ritual verification",
		"[ok] guidance installed",
		"3/3 platform guidance blocks current",
		"config generated",
		"tools visible",
		"no endpoint to probe (local stdio)",
		"no endpoint to query (local stdio)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("install --verify output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, fakeBearerToken) {
		t.Fatalf("install --verify output leaked token:\n%s", out)
	}
}

func TestAssistantInstallVerifyHonorsPlatformFilter(t *testing.T) {
	e := newTestEngine(t)
	p, ok := lookupPlatform("codex")
	if !ok {
		t.Fatal("codex platform missing")
	}
	results, err := e.install([]assistantPlatform{p})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	out := captureStdout(t, func() {
		if err := renderAssistantInstall(e.root, results, true); err != nil {
			t.Fatalf("renderAssistantInstall(--verify) error = %v", err)
		}
	})
	if !strings.Contains(out, "1/1 platform guidance blocks current") {
		t.Fatalf("filtered install verify did not report 1/1:\n%s", out)
	}
	if strings.Contains(out, "3/3 platform guidance blocks current") {
		t.Fatalf("filtered install verify reported all platforms:\n%s", out)
	}
}

func TestAssistantInstallCommandHasVerifyFlag(t *testing.T) {
	var installCmd *cobra.Command
	for _, cmd := range assistantCmd.Commands() {
		if cmd.Name() == "install" {
			installCmd = cmd
			break
		}
	}
	if installCmd == nil {
		t.Fatal("assistant install command not registered")
	}
	if flag := installCmd.Flags().Lookup("verify"); flag == nil {
		t.Fatal("assistant install command missing --verify flag")
	}
}
