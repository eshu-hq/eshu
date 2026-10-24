// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/component"
)

// TestComponentPolicyFlagsFallBackToEnvironment proves `component list` and
// the other policy-aware commands honor ESHU_COMPONENT_TRUST_MODE and the
// allow and revoke lists, and that an explicitly passed flag wins.
func TestComponentPolicyFlagsFallBackToEnvironment(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envComponentTrustMode:        "allowlist",
		envComponentAllowIDs:         "dev.example.one, dev.example.two,,",
		envComponentAllowPublishers:  "eshu-hq",
		envComponentRevokeIDs:        "dev.example.bad",
		envComponentRevokePublishers: "mallory",
	}
	getenv := func(key string) string { return env[key] }

	newListCommand := func() *cobra.Command {
		cmd := &cobra.Command{}
		addOptionalTrustFlags(cmd)
		return cmd
	}

	t.Run("environment fills unset flags", func(t *testing.T) {
		t.Parallel()

		policy := componentPolicyFromFlagsAndEnv(newListCommand(), getenv)
		if got, want := policy.Mode, "allowlist"; got != want {
			t.Fatalf("Mode = %q, want %q", got, want)
		}
		if got, want := strings.Join(policy.AllowedIDs, "|"), "dev.example.one|dev.example.two"; got != want {
			t.Fatalf("AllowedIDs = %q, want %q", got, want)
		}
		if got, want := strings.Join(policy.AllowedPublishers, "|"), "eshu-hq"; got != want {
			t.Fatalf("AllowedPublishers = %q, want %q", got, want)
		}
		if got, want := strings.Join(policy.RevokedIDs, "|"), "dev.example.bad"; got != want {
			t.Fatalf("RevokedIDs = %q, want %q", got, want)
		}
		if got, want := strings.Join(policy.RevokedPublishers, "|"), "mallory"; got != want {
			t.Fatalf("RevokedPublishers = %q, want %q", got, want)
		}
	})

	t.Run("passed flags win over the environment", func(t *testing.T) {
		t.Parallel()

		cmd := newListCommand()
		if err := cmd.Flags().Parse([]string{"--trust-mode", "disabled", "--allow-id", "flag.id"}); err != nil {
			t.Fatalf("Parse() error = %v, want nil", err)
		}
		policy := componentPolicyFromFlagsAndEnv(cmd, getenv)
		if got, want := policy.Mode, "disabled"; got != want {
			t.Fatalf("Mode = %q, want %q", got, want)
		}
		if got, want := strings.Join(policy.AllowedIDs, "|"), "flag.id"; got != want {
			t.Fatalf("AllowedIDs = %q, want %q", got, want)
		}
		if got, want := strings.Join(policy.AllowedPublishers, "|"), "eshu-hq"; got != want {
			t.Fatalf("AllowedPublishers = %q, want the environment value for the unpassed flag", got)
		}
	})

	// A deny list must never be emptied by a flag: a revoke flag adds to the
	// environment revoke list instead of replacing it.
	t.Run("passed revoke flags add to the environment revoke lists", func(t *testing.T) {
		t.Parallel()

		cmd := newListCommand()
		args := []string{"--revoke-id", "flag.bad", "--revoke-publisher", "flag-mallory"}
		if err := cmd.Flags().Parse(args); err != nil {
			t.Fatalf("Parse() error = %v, want nil", err)
		}
		policy := componentPolicyFromFlagsAndEnv(cmd, getenv)
		if got, want := strings.Join(policy.RevokedIDs, "|"), "flag.bad|dev.example.bad"; got != want {
			t.Fatalf("RevokedIDs = %q, want %q: the environment id must stay revoked", got, want)
		}
		if got, want := strings.Join(policy.RevokedPublishers, "|"), "flag-mallory|mallory"; got != want {
			t.Fatalf("RevokedPublishers = %q, want %q: the environment publisher must stay revoked", got, want)
		}
	})

	t.Run("a revoke flag naming an environment entry does not duplicate it", func(t *testing.T) {
		t.Parallel()

		cmd := newListCommand()
		if err := cmd.Flags().Parse([]string{"--revoke-id", "dev.example.bad"}); err != nil {
			t.Fatalf("Parse() error = %v, want nil", err)
		}
		policy := componentPolicyFromFlagsAndEnv(cmd, getenv)
		if got, want := strings.Join(policy.RevokedIDs, "|"), "dev.example.bad"; got != want {
			t.Fatalf("RevokedIDs = %q, want %q", got, want)
		}
	})

	t.Run("empty environment keeps the flag defaults", func(t *testing.T) {
		t.Parallel()

		cmd := &cobra.Command{}
		addTrustFlags(cmd)
		policy := componentPolicyFromFlagsAndEnv(cmd, func(string) string { return "" })
		if got, want := policy.Mode, component.TrustModeDisabled; got != want {
			t.Fatalf("Mode = %q, want the verify and install default %q", got, want)
		}
		if len(policy.AllowedIDs) != 0 {
			t.Fatalf("AllowedIDs = %v, want none", policy.AllowedIDs)
		}
	})
}

// TestComponentListWithoutPolicyDoesNotReverifyInstalledComponents is the
// regression for the `component list` false failure: with no trust flag and
// no trust variable the policy must stay zero, so readback reports stored
// lifecycle state only. The provenance predicate-type flag default used to
// make the policy non-zero, which re-verified every installed component in
// disabled mode and marked it failed with untrusted_publisher.
func TestComponentListWithoutPolicyDoesNotReverifyInstalledComponents(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	installCmd := newComponentTestCommand(&bytes.Buffer{})
	installCmd.Flags().String(componentHomeFlag, home, "")
	installCmd.Flags().String(componentTrustModeFlag, component.TrustModeAllowlist, "")
	installCmd.Flags().StringSlice(componentAllowIDFlag, []string{"dev.eshu.collector.aws"}, "")
	installCmd.Flags().StringSlice(componentAllowPublisherFlag, []string{"eshu-hq"}, "")
	if err := runComponentInstall(installCmd, []string{writeComponentCommandManifest(t)}); err != nil {
		t.Fatalf("runComponentInstall() error = %v, want nil", err)
	}

	listCmd := &cobra.Command{}
	addOptionalTrustFlags(listCmd)
	policy := componentPolicyFromFlagsAndEnv(listCmd, func(string) string { return "" })
	readback, err := component.NewRegistry(home).Readback(policy)
	if err != nil {
		t.Fatalf("Readback() error = %v, want nil", err)
	}
	if len(readback) != 1 {
		t.Fatalf("Readback() count = %d, want 1", len(readback))
	}
	if got, want := strings.Join(readback[0].States, ","), component.RegistryStateInstalled; got != want {
		t.Fatalf("states = %q, want %q: an unconfigured list must not re-verify", got, want)
	}
	if readback[0].Verification != nil {
		t.Fatalf("Verification = %+v, want nil without a configured policy", readback[0].Verification)
	}
}

// TestComponentEnvironmentRevocationSurvivesRevokeFlags is the verdict-level
// regression for the deny-list rule: a component that ESHU_COMPONENT_REVOKE_IDS
// or ESHU_COMPONENT_REVOKE_PUBLISHERS blocks stays blocked when the allow flags
// would admit it, and when a revoke flag names something else.
func TestComponentEnvironmentRevocationSurvivesRevokeFlags(t *testing.T) {
	t.Parallel()

	manifest, err := component.LoadManifest(writeComponentCommandManifest(t))
	if err != nil {
		t.Fatalf("LoadManifest() error = %v, want nil", err)
	}
	allowFlags := []string{
		"--trust-mode", component.TrustModeAllowlist,
		"--allow-id", manifest.Metadata.ID,
		"--allow-publisher", manifest.Metadata.Publisher,
	}

	tests := []struct {
		name  string
		env   map[string]string
		extra []string
	}{
		{name: "environment id revoke with allow flags", env: map[string]string{envComponentRevokeIDs: manifest.Metadata.ID}},
		{name: "environment id revoke with an unrelated revoke flag", env: map[string]string{envComponentRevokeIDs: manifest.Metadata.ID}, extra: []string{"--revoke-id", "dev.example.other"}},
		{name: "environment publisher revoke with allow flags", env: map[string]string{envComponentRevokePublishers: manifest.Metadata.Publisher}},
		{name: "environment publisher revoke with an unrelated revoke flag", env: map[string]string{envComponentRevokePublishers: manifest.Metadata.Publisher}, extra: []string{"--revoke-publisher", "other-publisher"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := &cobra.Command{}
			addOptionalTrustFlags(cmd)
			if err := cmd.Flags().Parse(append(append([]string{}, allowFlags...), tt.extra...)); err != nil {
				t.Fatalf("Parse() error = %v, want nil", err)
			}
			policy := componentPolicyFromFlagsAndEnv(cmd, func(key string) string { return tt.env[key] })
			result := policy.Verify(manifest)
			if result.Allowed || result.Code != component.ErrorCodeRevokedPackage {
				t.Fatalf("Verify() = allowed %t code %q, want revoked", result.Allowed, result.Code)
			}
		})
	}
}
