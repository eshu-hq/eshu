// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// The guidance logic itself lives in internal/cli/assistantguidance and is
// tested there. These tests cover what only the wrapper owns: cobra
// registration, flag wiring, root resolution from process state, and the fact
// that command output lands on the stream cobra resolved.

func assistantSubcommand(t *testing.T, name string) *cobra.Command {
	t.Helper()
	for _, cmd := range assistantCmd.Commands() {
		if cmd.Name() == name {
			return cmd
		}
	}
	t.Fatalf("assistant %s command not registered", name)
	return nil
}

func TestAssistantStatusCommandHasVerifyFlag(t *testing.T) {
	if flag := assistantSubcommand(t, "status").Flags().Lookup("verify"); flag == nil {
		t.Fatal("assistant status command missing --verify flag")
	}
}

func TestAssistantInstallCommandHasVerifyFlag(t *testing.T) {
	if flag := assistantSubcommand(t, "install").Flags().Lookup("verify"); flag == nil {
		t.Fatal("assistant install command missing --verify flag")
	}
}

func TestAssistantCommandsRegistered(t *testing.T) {
	for _, name := range []string{"install", "status", "uninstall"} {
		if cmd := assistantSubcommand(t, name); cmd.RunE == nil {
			t.Fatalf("assistant %s has no RunE", name)
		}
	}
	for _, name := range []string{"path", "platform"} {
		if flag := assistantCmd.PersistentFlags().Lookup(name); flag == nil {
			t.Fatalf("assistant command missing persistent --%s flag", name)
		}
	}
}

func TestResolveRootUsesFlagThenWorkingDirectory(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	got, err := resolveRoot("")
	if err != nil {
		t.Fatalf("resolveRoot(\"\"): %v", err)
	}
	if got != wd {
		t.Fatalf("empty --path should resolve to the working directory: got %q want %q", got, wd)
	}

	abs := t.TempDir()
	if got, err = resolveRoot(abs); err != nil || got != abs {
		t.Fatalf("resolveRoot(%q) = %q, %v", abs, got, err)
	}

	// A relative --path is resolved against the same working directory.
	if got, err = resolveRoot("sub/dir"); err != nil {
		t.Fatalf("resolveRoot relative: %v", err)
	}
	if want := filepath.Join(wd, "sub", "dir"); got != want {
		t.Fatalf("relative --path resolved to %q, want %q", got, want)
	}
}

// TestRunAssistantInstallWritesToResolvedStream drives the real RunE through
// cobra and asserts the guidance landed on disk and the summary landed on the
// stream cobra resolved -- the two things the wrapper is responsible for.
func TestRunAssistantInstallWritesToResolvedStream(t *testing.T) {
	root := t.TempDir()
	t.Cleanup(func() {
		assistantGuidanceRoot = ""
		assistantPlatformFilter = ""
		assistantInstallVerify = false
	})
	assistantGuidanceRoot = root
	assistantPlatformFilter = "claude"
	assistantInstallVerify = false

	var out bytes.Buffer
	cmd := assistantSubcommand(t, "install")
	cmd.SetOut(&out)
	t.Cleanup(func() { cmd.SetOut(nil) })

	if err := runAssistantInstall(cmd, nil); err != nil {
		t.Fatalf("runAssistantInstall: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "created CLAUDE.md with Eshu guidance") {
		t.Fatalf("install summary did not reach the resolved stream:\n%s", got)
	}
	data, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read installed guidance: %v", err)
	}
	if !strings.Contains(string(data), "BEGIN ESHU GUIDANCE") {
		t.Fatalf("guidance block not written:\n%s", data)
	}

	// The --platform filter is honored: no AGENTS.md was created.
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("--platform filter ignored, AGENTS.md stat err=%v", err)
	}
}

func TestRunAssistantCommandsRejectUnsupportedPlatform(t *testing.T) {
	t.Cleanup(func() {
		assistantGuidanceRoot = ""
		assistantPlatformFilter = ""
	})
	assistantGuidanceRoot = t.TempDir()
	assistantPlatformFilter = "jetbrains"

	for name, run := range map[string]func(*cobra.Command, []string) error{
		"install":   runAssistantInstall,
		"status":    runAssistantStatus,
		"uninstall": runAssistantUninstall,
	} {
		cmd := assistantSubcommand(t, name)
		cmd.SetOut(&bytes.Buffer{})
		err := run(cmd, nil)
		cmd.SetOut(nil)
		if err == nil {
			t.Fatalf("%s accepted an unsupported --platform", name)
		}
		if !strings.Contains(err.Error(), "unsupported assistant platform") {
			t.Fatalf("%s error = %v, want the unsupported-platform message", name, err)
		}
	}
}
