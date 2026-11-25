// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestRunAPIStartExecsCoreAPIWithAddr proves the `eshu api start` delegate is
// the pool-owner core binary, with the CLI's --host/--port carried on the
// --addr flag eshu-core-api actually reads (it takes no ESHU_API_ADDR env).
func TestRunAPIStartExecsCoreAPIWithAddr(t *testing.T) {
	t.Setenv("ESHU_API_ADDR", "")

	restore, calls := stubServiceRuntime()
	defer restore()
	calls.inheritEnv = true

	const resolved = "/tmp/eshu-core-api"
	calls.lookPath = func(binary string) (string, error) {
		if binary != "eshu-core-api" {
			t.Fatalf("LookPath(%q), want eshu-core-api", binary)
		}
		return resolved, nil
	}
	calls.exec = func(binary string, args []string, env []string) error {
		calls.binary = binary
		calls.args = append([]string(nil), args...)
		calls.env = append([]string(nil), env...)
		return nil
	}

	cmd := newAPIStartTestCommand()
	if err := cmd.Flags().Set("host", "127.0.0.1"); err != nil {
		t.Fatalf("Set(host) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("port", "9090"); err != nil {
		t.Fatalf("Set(port) error = %v, want nil", err)
	}

	if err := runAPIStart(cmd, nil); err != nil {
		t.Fatalf("runAPIStart() error = %v, want nil", err)
	}

	if calls.binary != resolved {
		t.Fatalf("exec binary = %q, want %q", calls.binary, resolved)
	}
	if len(calls.args) == 0 || calls.args[0] != "eshu-core-api" {
		t.Fatalf("exec argv[0] = %#v, want eshu-core-api", calls.args)
	}
	assertArgValue(t, calls.args, "--addr", "127.0.0.1:9090")
	assertEnvValue(t, calls.env, "ESHU_API_ADDR", "127.0.0.1:9090")
}

// TestRunServeStartExecsCoreAPIWithAddr proves the `eshu serve start`
// delegate is the same pool-owner core binary with the same --addr wiring.
func TestRunServeStartExecsCoreAPIWithAddr(t *testing.T) {
	t.Setenv("ESHU_API_ADDR", "")

	restore, calls := stubServiceRuntime()
	defer restore()
	calls.inheritEnv = true

	const resolved = "/tmp/eshu-core-api"
	calls.lookPath = func(binary string) (string, error) {
		if binary != "eshu-core-api" {
			t.Fatalf("LookPath(%q), want eshu-core-api", binary)
		}
		return resolved, nil
	}
	calls.exec = func(binary string, args []string, env []string) error {
		calls.binary = binary
		calls.args = append([]string(nil), args...)
		calls.env = append([]string(nil), env...)
		return nil
	}

	cmd := newServeStartTestCommand()
	if err := cmd.Flags().Set("host", "127.0.0.1"); err != nil {
		t.Fatalf("Set(host) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("port", "9091"); err != nil {
		t.Fatalf("Set(port) error = %v, want nil", err)
	}

	if err := runServeStart(cmd, nil); err != nil {
		t.Fatalf("runServeStart() error = %v, want nil", err)
	}

	if calls.binary != resolved {
		t.Fatalf("exec binary = %q, want %q", calls.binary, resolved)
	}
	if len(calls.args) == 0 || calls.args[0] != "eshu-core-api" {
		t.Fatalf("exec argv[0] = %#v, want eshu-core-api", calls.args)
	}
	assertArgValue(t, calls.args, "--addr", "127.0.0.1:9091")
	assertEnvValue(t, calls.env, "ESHU_API_ADDR", "127.0.0.1:9091")
}

// TestRunAPIStartCoreAPIMissingFromPath proves a missing core binary fails
// before any exec, naming the binary the operator must install.
func TestRunAPIStartCoreAPIMissingFromPath(t *testing.T) {
	restore, calls := stubServiceRuntime()
	defer restore()
	calls.lookPath = func(string) (string, error) {
		return "", errors.New("executable file not found in $PATH")
	}
	calls.exec = func(string, []string, []string) error {
		t.Fatal("exec called, want missing-binary error instead")
		return nil
	}

	err := runAPIStart(newAPIStartTestCommand(), nil)
	if err == nil || !strings.Contains(err.Error(), "eshu-core-api") {
		t.Fatalf("runAPIStart() error = %v, want eshu-core-api complaint", err)
	}
}

// TestRunServeStartCoreAPIMissingFromPath proves a missing core binary fails
// the serve delegate before any exec, mirroring the api-delegate coverage.
func TestRunServeStartCoreAPIMissingFromPath(t *testing.T) {
	restore, calls := stubServiceRuntime()
	defer restore()
	calls.lookPath = func(string) (string, error) {
		return "", errors.New("executable file not found in $PATH")
	}
	calls.exec = func(string, []string, []string) error {
		t.Fatal("exec called, want missing-binary error instead")
		return nil
	}

	err := runServeStart(newServeStartTestCommand(), nil)
	if err == nil || !strings.Contains(err.Error(), "eshu-core-api") {
		t.Fatalf("runServeStart() error = %v, want eshu-core-api complaint", err)
	}
}

// newAPIStartTestCommand builds a standalone command carrying the flags
// runAPIStart reads, without reaching the shared command tree.
func newAPIStartTestCommand() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("host", "127.0.0.1", "")
	cmd.Flags().Int("port", 8080, "")
	return cmd
}

// newServeStartTestCommand builds a standalone command carrying the flags
// runServeStart reads, without reaching the shared command tree.
func newServeStartTestCommand() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("host", "0.0.0.0", "")
	cmd.Flags().Int("port", 8080, "")
	return cmd
}
