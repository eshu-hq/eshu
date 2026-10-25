// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/component"
)

// mainBranchBuildVersion is what a main-branch image reports: docker-publish
// stamps buildinfo.Version with the commit SHA, which is not semver (#9090).
const mainBranchBuildVersion = "19785f70bb80f9ebd5326773c7f526e69d95e936"

// TestComponentInstallHonorsCoreVersionOverride drives `eshu component
// install` the way an activation reconcile init container does: a component
// whose compatibleCore is ">=0.0.5 <0.1.0", on a build whose version is a
// commit SHA. Before #9090 the CLI ignored ESHU_COMPONENT_CORE_VERSION and
// refused every such install with "is not semantic version".
//
// Not parallel: it sets process environment and buildinfo.Version, and
// runComponentInstall reads both.
func TestComponentInstallHonorsCoreVersionOverride(t *testing.T) {
	cases := []struct {
		name      string
		env       string
		args      []string
		wantError string
	}{
		{name: "env override inside the range installs", env: "0.0.9"},
		{name: "env override outside the range is incompatible", env: "0.3.0", wantError: "incompatible with Eshu core"},
		{name: "no override keeps refusing a non-semver build", wantError: "is not semantic version"},
		{name: "flag wins over the env var", env: "0.3.0", args: []string{"--core-version", "0.0.7"}},
		{name: "flag outside the range is incompatible", env: "0.0.9", args: []string{"--core-version", "0.1.0"}, wantError: "incompatible with Eshu core"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original := buildinfo.Version
			buildinfo.Version = mainBranchBuildVersion
			t.Cleanup(func() { buildinfo.Version = original })
			t.Setenv("ESHU_COMPONENT_CORE_VERSION", tc.env)

			out := &bytes.Buffer{}
			cmd := newComponentTestCommand(out)
			cmd.Flags().String(componentHomeFlag, t.TempDir(), "")
			addTrustFlags(cmd)
			args := append([]string{
				"--trust-mode", "allowlist",
				"--allow-id", "dev.eshu.collector.aws",
				"--allow-publisher", "eshu-hq",
			}, tc.args...)
			if err := cmd.Flags().Parse(args); err != nil {
				t.Fatalf("Parse(%v) error = %v, want nil", args, err)
			}

			err := runComponentInstall(cmd, []string{writeComponentCommandManifest(t)})
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("runComponentInstall() error = %v, want nil", err)
				}
				if !strings.Contains(out.String(), "installed dev.eshu.collector.aws@0.1.0") {
					t.Fatalf("install output = %q, want the installed line", out.String())
				}
				return
			}
			if err == nil || !strings.Contains(err.Error()+out.String(), tc.wantError) {
				t.Fatalf("runComponentInstall() error = %v output = %q, want %q", err, out.String(), tc.wantError)
			}
		})
	}
}

// TestComponentListCoreVersionAloneDoesNotReverify keeps the zero-policy rule
// for `component list`: a core version is a compatibility input, not a trust
// input, so setting only ESHU_COMPONENT_CORE_VERSION or --core-version must not
// re-verify installed components under a disabled trust mode. The activation
// reconcile script runs `eshu component list` with exactly that environment.
func TestComponentListCoreVersionAloneDoesNotReverify(t *testing.T) {
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

	cases := []struct {
		name string
		env  map[string]string
		args []string
	}{
		{name: "env only", env: map[string]string{envComponentCoreVersion: "0.0.9"}},
		{name: "flag only", args: []string{"--core-version", "0.0.9"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			listCmd := &cobra.Command{}
			addOptionalTrustFlags(listCmd)
			if err := listCmd.Flags().Parse(tc.args); err != nil {
				t.Fatalf("Parse(%v) error = %v, want nil", tc.args, err)
			}
			policy := componentPolicyFromFlagsAndEnv(listCmd, func(key string) string { return tc.env[key] })
			readback, err := component.NewRegistry(home).Readback(policy)
			if err != nil {
				t.Fatalf("Readback() error = %v, want nil", err)
			}
			if len(readback) != 1 {
				t.Fatalf("Readback() count = %d, want 1", len(readback))
			}
			if got, want := strings.Join(readback[0].States, ","), component.RegistryStateInstalled; got != want {
				t.Fatalf("states = %q, want %q: a core version alone must not re-verify", got, want)
			}
			if readback[0].Verification != nil {
				t.Fatalf("Verification = %+v, want nil with no trust input configured", readback[0].Verification)
			}
		})
	}
}
