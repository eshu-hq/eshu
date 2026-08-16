// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// The config family's logic lives in internal/cli/config, which owns its own
// tests. What stays testable only from here is the cobra wiring: that each
// registered subcommand actually calls into that package and prints what the
// operator sees. A wrapper that resolved flags and then called nothing would
// still pass the package's tests, so these exercise the registered RunE.

// configSubcommand returns the registered `eshu config <name>` command, failing
// the test when the subcommand was never registered.
func configSubcommand(t *testing.T, name string) *cobra.Command {
	t.Helper()
	for _, sub := range configCmd.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	t.Fatalf("config subcommand %q is not registered; registered: %v", name, configSubcommandNames())
	return nil
}

func configSubcommandNames() []string {
	names := make([]string, 0, len(configCmd.Commands()))
	for _, sub := range configCmd.Commands() {
		names = append(names, sub.Name())
	}
	return names
}

func TestConfigSetThenShowRoundTripsThroughTheStore(t *testing.T) {
	t.Setenv("ESHU_HOME", t.TempDir())

	setCmd := configSubcommand(t, "set")
	setOut := captureStdout(t, func() {
		if err := setCmd.RunE(setCmd, []string{"ESHU_API_KEY", "wired-token"}); err != nil {
			t.Fatalf("config set RunE error = %v, want nil", err)
		}
	})
	if !strings.Contains(setOut, "Set ESHU_API_KEY") {
		t.Fatalf("config set output = %q, want the confirmation line", setOut)
	}

	showCmd := configSubcommand(t, "show")
	showOut := captureStdout(t, func() {
		if err := showCmd.RunE(showCmd, nil); err != nil {
			t.Fatalf("config show RunE error = %v, want nil", err)
		}
	})
	if !strings.Contains(showOut, "ESHU_API_KEY") || !strings.Contains(showOut, "wired-token") {
		t.Fatalf("config show output = %q, want the key set moments earlier", showOut)
	}
}

func TestConfigShowReportsTheEmptyStorePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ESHU_HOME", home)

	showCmd := configSubcommand(t, "show")
	out := captureStdout(t, func() {
		if err := showCmd.RunE(showCmd, nil); err != nil {
			t.Fatalf("config show RunE error = %v, want nil", err)
		}
	})
	if !strings.Contains(out, "No configuration found at") || !strings.Contains(out, home) {
		t.Fatalf("config show output = %q, want the missing-config notice naming %q", out, home)
	}
}

func TestConfigDBWritesBackendSelection(t *testing.T) {
	t.Setenv("ESHU_HOME", t.TempDir())

	dbCmd := configSubcommand(t, "db")
	out := captureStdout(t, func() {
		if err := dbCmd.RunE(dbCmd, []string{"nornic"}); err != nil {
			t.Fatalf("config db RunE error = %v, want nil", err)
		}
	})
	if !strings.Contains(out, "Default database switched to nornicdb") {
		t.Fatalf("config db output = %q, want the canonical backend name", out)
	}

	showOut := captureStdout(t, func() {
		showCmd := configSubcommand(t, "show")
		if err := showCmd.RunE(showCmd, nil); err != nil {
			t.Fatalf("config show RunE error = %v, want nil", err)
		}
	})
	if !strings.Contains(showOut, "ESHU_GRAPH_BACKEND") || !strings.Contains(showOut, "nornicdb") {
		t.Fatalf("config show output = %q, want the persisted backend selection", showOut)
	}
}

func TestConfigDBRejectsUnknownBackend(t *testing.T) {
	t.Setenv("ESHU_HOME", t.TempDir())

	dbCmd := configSubcommand(t, "db")
	err := dbCmd.RunE(dbCmd, []string{"postgres"})
	if err == nil {
		t.Fatal("config db RunE error = nil, want a rejection for an unknown backend")
	}
	if !strings.Contains(err.Error(), "invalid backend: postgres") {
		t.Fatalf("config db error = %q, want it to name the rejected backend", err)
	}
}

func TestRunConfigValidateReferenceFlagPrintsTheRegistryDoc(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("strict", false, "")
	cmd.Flags().Bool("reference", true, "")
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runConfigValidate(cmd, nil); err != nil {
		t.Fatalf("runConfigValidate() error = %v, want nil", err)
	}
	if !strings.Contains(out.String(), "ESHU_") {
		t.Fatalf("--reference output = %q, want the generated variable reference", out.String())
	}
}

func TestRunConfigValidateReportsInvalidProcessEnvironment(t *testing.T) {
	// The wrapper's job is to snapshot the real process environment; setting a
	// bad value here proves it reads os.Environ rather than an empty map.
	t.Setenv("ESHU_POSTGRES_MAX_OPEN_CONNS", "not-a-number")

	cmd := &cobra.Command{}
	cmd.Flags().Bool("strict", false, "")
	cmd.Flags().Bool("reference", false, "")
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runConfigValidate(cmd, nil)
	if err == nil {
		t.Fatal("runConfigValidate() error = nil, want a non-zero exit for an invalid value")
	}
	if !strings.Contains(out.String(), "ESHU_POSTGRES_MAX_OPEN_CONNS") {
		t.Fatalf("validate output = %q, want the offending variable named", out.String())
	}
}
