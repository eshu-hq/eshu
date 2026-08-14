// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package assistantguidance

import (
	"bytes"
	"strings"
	"testing"
)

// fakeBearerToken mirrors the credential-shaped literal go/cmd/eshu's MCP setup
// tests use, so the assertions that no token reaches assistant output survive
// the move out of package main.
const fakeBearerToken = "eshu_live_SUPERSECRET_abc123def456ghi789"

func renderToString(t *testing.T, fn func(w *bytes.Buffer) error, wantErr bool) string {
	t.Helper()
	var buf bytes.Buffer
	err := fn(&buf)
	switch {
	case wantErr && err == nil:
		t.Fatal("expected an error, got nil")
	case !wantErr && err != nil:
		t.Fatalf("unexpected error: %v", err)
	}
	return buf.String()
}

func TestStatusDefaultOmitsVerification(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.Install(SupportedPlatforms()); err != nil {
		t.Fatalf("install: %v", err)
	}
	results, err := e.Status(SupportedPlatforms())
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	out := renderToString(t, func(w *bytes.Buffer) error {
		return RenderStatus(w, e.Root(), results, false)
	}, false)
	if strings.Contains(out, "Assistant ritual verification") {
		t.Fatalf("default status should not print verification block:\n%s", out)
	}
	if !strings.Contains(out, "Claude Code") || !strings.Contains(out, "current") {
		t.Fatalf("default status missing platform table:\n%s", out)
	}
}

func TestStatusVerifyReportsLocalStdioDiagnostics(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.Install(SupportedPlatforms()); err != nil {
		t.Fatalf("install: %v", err)
	}
	results, err := e.Status(SupportedPlatforms())
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	out := renderToString(t, func(w *bytes.Buffer) error {
		return RenderStatus(w, e.Root(), results, true)
	}, false)
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

func TestStatusVerifyFailsWhenGuidanceMissing(t *testing.T) {
	e := newTestEngine(t)
	p := claudePlatform(t)
	results, err := e.Status([]Platform{p})
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	out := renderToString(t, func(w *bytes.Buffer) error {
		return RenderStatus(w, e.Root(), results, true)
	}, true)
	if !strings.Contains(out, "[!!] guidance installed") {
		t.Fatalf("missing guidance should fail verification:\n%s", out)
	}
	if !strings.Contains(out, "0/1 platform guidance blocks current") {
		t.Fatalf("missing guidance count not reported:\n%s", out)
	}
}

func TestInstallDefaultOmitsVerification(t *testing.T) {
	e := newTestEngine(t)
	results, err := e.Install(SupportedPlatforms())
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	out := renderToString(t, func(w *bytes.Buffer) error {
		return RenderInstall(w, e.Root(), results, false)
	}, false)
	if strings.Contains(out, "Assistant ritual verification") {
		t.Fatalf("default install should not print verification block:\n%s", out)
	}
	if !strings.Contains(out, "created CLAUDE.md with Eshu guidance") {
		t.Fatalf("default install missing install result:\n%s", out)
	}
}

func TestInstallVerifyReportsLocalStdioDiagnostics(t *testing.T) {
	e := newTestEngine(t)
	results, err := e.Install(SupportedPlatforms())
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	out := renderToString(t, func(w *bytes.Buffer) error {
		return RenderInstall(w, e.Root(), results, true)
	}, false)
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

func TestInstallVerifyHonorsPlatformFilter(t *testing.T) {
	e := newTestEngine(t)
	p, ok := LookupPlatform("codex")
	if !ok {
		t.Fatal("codex platform missing")
	}
	results, err := e.Install([]Platform{p})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	out := renderToString(t, func(w *bytes.Buffer) error {
		return RenderInstall(w, e.Root(), results, true)
	}, false)
	if !strings.Contains(out, "1/1 platform guidance blocks current") {
		t.Fatalf("filtered install verify did not report 1/1:\n%s", out)
	}
	if strings.Contains(out, "3/3 platform guidance blocks current") {
		t.Fatalf("filtered install verify reported all platforms:\n%s", out)
	}
}

// TestRenderInstallGitAddHints pins the exact commit-hint block, including its
// sorted order and two-space indent, because operators copy those lines.
func TestRenderInstallGitAddHints(t *testing.T) {
	e := newTestEngine(t)
	results, err := e.Install(SupportedPlatforms())
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	out := renderToString(t, func(w *bytes.Buffer) error {
		return RenderInstall(w, e.Root(), results, false)
	}, false)
	want := "\nCommit the guidance so teammates and CI agents share it:\n" +
		"  git add .cursor/rules/eshu.mdc\n" +
		"  git add AGENTS.md\n" +
		"  git add CLAUDE.md\n"
	if !strings.HasSuffix(out, want) {
		t.Fatalf("commit hints not rendered as expected:\n want suffix=%q\n got=%q", want, out)
	}

	// A second install changes nothing, so no hints at all.
	again, err := e.Install(SupportedPlatforms())
	if err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	out = renderToString(t, func(w *bytes.Buffer) error {
		return RenderInstall(w, e.Root(), again, false)
	}, false)
	if strings.Contains(out, "git add") {
		t.Fatalf("unchanged reinstall should print no commit hints:\n%s", out)
	}
	if !strings.Contains(out, "already current (current)") {
		t.Fatalf("unchanged reinstall missing the already-current line:\n%s", out)
	}
}

// TestRenderUninstallCoversEveryOutcome exercises all three uninstall lines:
// a deleted Eshu-created file, a stripped file that keeps user content, and a
// file with no block at all.
func TestRenderUninstallCoversEveryOutcome(t *testing.T) {
	e := newTestEngine(t)
	results := []Result{
		{Platform: Platform{Label: "Claude Code"}, Path: e.Root() + "/CLAUDE.md", Removed: true, Changed: true},
		{Platform: Platform{Label: "Codex / AGENTS.md"}, Path: e.Root() + "/AGENTS.md", Changed: true},
		{Platform: Platform{Label: "Cursor"}, Path: e.Root() + "/.cursor/rules/eshu.mdc"},
	}

	var buf bytes.Buffer
	RenderUninstall(&buf, e.Root(), results)
	want := "OK Claude Code: removed Eshu-created CLAUDE.md\n" +
		"OK Codex / AGENTS.md: removed Eshu guidance block from AGENTS.md\n" +
		"- Cursor: no Eshu guidance block in .cursor/rules/eshu.mdc\n"
	if got := buf.String(); got != want {
		t.Fatalf("uninstall output:\n want=%q\n  got=%q", want, got)
	}
}

func TestRelOrPathFallsBackToAbsolute(t *testing.T) {
	if got := RelOrPath("/a/b", "/a/b/c.md"); got != "c.md" {
		t.Fatalf("RelOrPath = %q, want c.md", got)
	}
	// A relative root against an absolute path cannot be related; the absolute
	// path is returned unchanged rather than a misleading ../.. chain.
	if got := RelOrPath("relative/root", "/abs/path.md"); got != "/abs/path.md" {
		t.Fatalf("RelOrPath = %q, want the absolute path back", got)
	}
}
