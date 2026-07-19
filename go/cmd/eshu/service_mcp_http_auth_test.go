// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// TestRunMCPStartHTTPWithoutWorkspaceRootDefaultsAllowUnauthenticated proves
// the SAME default applies to the direct (no --workspace-root, no running
// owner to attach to) HTTP start path -- both HTTP code paths in
// runMCPStart must inject the escape hatch (#5168).
func TestRunMCPStartHTTPWithoutWorkspaceRootDefaultsAllowUnauthenticated(t *testing.T) {
	restore, calls := stubServiceRuntime()
	defer restore()

	calls.lookPath = func(binary string) (string, error) {
		if binary != "eshu-mcp-server" {
			t.Fatalf("LookPath(%q), want eshu-mcp-server", binary)
		}
		return "/tmp/eshu-mcp-server", nil
	}
	calls.exec = func(binary string, args []string, env []string) error {
		calls.binary = binary
		calls.args = append([]string(nil), args...)
		calls.env = append([]string(nil), env...)
		return nil
	}

	cmd := newMCPStartTestCommand()
	if err := cmd.Flags().Set("transport", "http"); err != nil {
		t.Fatalf("Set(transport) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("host", "127.0.0.1"); err != nil {
		t.Fatalf("Set(host) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("port", "8081"); err != nil {
		t.Fatalf("Set(port) error = %v, want nil", err)
	}

	if err := runMCPStart(cmd, nil); err != nil {
		t.Fatalf("runMCPStart() error = %v, want nil", err)
	}

	assertEnvValue(t, calls.env, "ESHU_MCP_TRANSPORT", "http")
	assertEnvValue(t, calls.env, "ESHU_MCP_ADDR", "127.0.0.1:8081")
	assertEnvValue(t, calls.env, "ESHU_MCP_ALLOW_UNAUTHENTICATED", "true")
}

// TestRunMCPStartHTTPRespectsExplicitAllowUnauthenticatedOverride proves the
// local CLI default never clobbers an operator's own explicit choice --
// someone testing the strict "no silent open mode" gate locally by exporting
// ESHU_MCP_ALLOW_UNAUTHENTICATED=false must see that value reach the child
// process unchanged. A loopback host is used so the CLI default WOULD apply,
// making this a genuine "explicit env wins over the default" check.
func TestRunMCPStartHTTPRespectsExplicitAllowUnauthenticatedOverride(t *testing.T) {
	restore, calls := stubServiceRuntime()
	defer restore()
	eshuEnviron = func() []string {
		return []string{"PATH=/tmp", "ESHU_MCP_ALLOW_UNAUTHENTICATED=false"}
	}

	calls.lookPath = func(string) (string, error) { return "/tmp/eshu-mcp-server", nil }
	calls.exec = func(binary string, args []string, env []string) error {
		calls.env = append([]string(nil), env...)
		return nil
	}

	cmd := newMCPStartTestCommand()
	if err := cmd.Flags().Set("transport", "http"); err != nil {
		t.Fatalf("Set(transport) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("host", "127.0.0.1"); err != nil {
		t.Fatalf("Set(host) error = %v, want nil", err)
	}

	if err := runMCPStart(cmd, nil); err != nil {
		t.Fatalf("runMCPStart() error = %v, want nil", err)
	}

	assertEnvValue(t, calls.env, "ESHU_MCP_ALLOW_UNAUTHENTICATED", "false")
}

// TestRunMCPStartHTTPNonLoopbackHostDoesNotDefaultAllowUnauthenticated is the
// issue #5168 review P1 regression: the CLI escape-hatch default must NOT be
// injected when the server binds a non-loopback interface. Helm runs
// `eshu mcp start --transport http` with the cobra default host 0.0.0.0 (all
// interfaces); if the CLI defaulted the escape hatch unconditionally, every
// Helm mcp-server pod would silently defeat the no-silent-open gate the one
// place -- a publicly reachable K8s Service -- it most needs to protect. With
// a non-loopback bind and no explicit env, the override must be absent so the
// startup gate governs (and refuses to start unless a real credential source
// is configured).
func TestRunMCPStartHTTPNonLoopbackHostDoesNotDefaultAllowUnauthenticated(t *testing.T) {
	restore, calls := stubServiceRuntime()
	defer restore()
	eshuEnviron = func() []string { return []string{"PATH=/tmp"} }

	calls.lookPath = func(string) (string, error) { return "/tmp/eshu-mcp-server", nil }
	calls.exec = func(binary string, args []string, env []string) error {
		calls.env = append([]string(nil), env...)
		return nil
	}

	cmd := newMCPStartTestCommand()
	if err := cmd.Flags().Set("transport", "http"); err != nil {
		t.Fatalf("Set(transport) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("host", "0.0.0.0"); err != nil {
		t.Fatalf("Set(host) error = %v, want nil", err)
	}

	if err := runMCPStart(cmd, nil); err != nil {
		t.Fatalf("runMCPStart() error = %v, want nil", err)
	}

	if got := envValue(calls.env, "ESHU_MCP_ALLOW_UNAUTHENTICATED"); got != "" {
		t.Fatalf("ESHU_MCP_ALLOW_UNAUTHENTICATED = %q for 0.0.0.0 bind, want unset (gate must govern)", got)
	}
}
