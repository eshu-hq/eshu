// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
)

// TestParseBootstrapArgsForce is the #7982 regression: the CLI already passes
// --force through to eshu-bootstrap-index, but the binary reads argv only for
// the version flag, so a forced re-index is not forced.
func TestParseBootstrapArgsForce(t *testing.T) {
	t.Parallel()

	args, err := parseBootstrapArgs([]string{"--path", "/work/root", "--force"})
	if err != nil {
		t.Fatalf("parseBootstrapArgs() error = %v, want nil", err)
	}
	if !args.Force {
		t.Fatal("parseBootstrapArgs(--force).Force = false, want true")
	}
	if got, want := args.Path, "/work/root"; got != want {
		t.Fatalf("parseBootstrapArgs(--path).Path = %q, want %q", got, want)
	}
}

func TestParseBootstrapArgsForceEqualsForm(t *testing.T) {
	t.Parallel()

	args, err := parseBootstrapArgs([]string{"--force=true"})
	if err != nil {
		t.Fatalf("parseBootstrapArgs() error = %v, want nil", err)
	}
	if !args.Force {
		t.Fatal("parseBootstrapArgs(--force=true).Force = false, want true")
	}
}

func TestParseBootstrapArgsDefaultsOff(t *testing.T) {
	t.Parallel()

	args, err := parseBootstrapArgs([]string{"--path", "/work/root"})
	if err != nil {
		t.Fatalf("parseBootstrapArgs() error = %v, want nil", err)
	}
	if args.Force {
		t.Fatal("parseBootstrapArgs(no --force).Force = true, want false")
	}
}

func TestParseBootstrapArgsRejectsUnknownFlag(t *testing.T) {
	t.Parallel()

	if _, err := parseBootstrapArgs([]string{"--froce"}); err == nil {
		t.Fatal("parseBootstrapArgs(--froce) error = nil, want non-nil")
	}
}

func TestParseBootstrapArgsForceExplicitOff(t *testing.T) {
	t.Parallel()

	args, err := parseBootstrapArgs([]string{"--force=false"})
	if err != nil {
		t.Fatalf("parseBootstrapArgs() error = %v, want nil", err)
	}
	if args.Force {
		t.Fatal("parseBootstrapArgs(--force=false).Force = true, want false")
	}
}

func TestParseBootstrapArgsHelp(t *testing.T) {
	t.Parallel()

	for _, flag := range []string{"--help", "-h"} {
		args, err := parseBootstrapArgs([]string{flag})
		if err != nil {
			t.Fatalf("parseBootstrapArgs(%s) error = %v, want nil", flag, err)
		}
		if !args.ShowHelp {
			t.Fatalf("parseBootstrapArgs(%s).ShowHelp = false, want true", flag)
		}
	}
}

// TestParseBootstrapArgsPathValue pins the --path value contract: a trailing
// or blank value fails, and the value after the flag is consumed so it is not
// parsed as a flag of its own.
func TestParseBootstrapArgsPathValue(t *testing.T) {
	t.Parallel()

	for name, argv := range map[string][]string{
		"trailing --path":    {"--path"},
		"blank --path value": {"--path", "  "},
		"blank --path=":      {"--path="},
	} {
		if _, err := parseBootstrapArgs(argv); err == nil {
			t.Fatalf("parseBootstrapArgs(%s) error = nil, want missing value", name)
		}
	}

	args, err := parseBootstrapArgs([]string{"--path", "--force"})
	if err != nil {
		t.Fatalf("parseBootstrapArgs(--path --force) error = %v, want nil", err)
	}
	if args.Path != "--force" || args.Force {
		t.Fatalf("parseBootstrapArgs(--path --force) = %+v, want Path=--force and Force=false", args)
	}
}
