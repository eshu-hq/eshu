// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/demo"
)

// passingDemoEnvelope is a complete run that scores clean at a generous
// target, so each case below fails for exactly the reason it names.
func passingDemoEnvelope() demo.Envelope {
	return demo.Envelope{
		Data: demo.Result{
			Project: "eshu-demo",
			Ready:   true,
			FirstAnswer: demo.Answer{
				Question: "Which workload does the api-svc repository run in?",
				Answer:   "Workload api-svc (kind: service) is defined in repository api-svc.",
				Truth:    map[string]any{"level": "derived", "basis": "hybrid", "freshness": "fresh"},
			},
			PhaseMillis: map[string]int64{
				"preflight": 43, "build": 1204, "up": 203642, "ready": 54617, "first_answer": 83,
			},
			TotalMillis: 259589,
		},
		Truth: map[string]any{"level": "derived", "basis": "hybrid", "freshness": "fresh"},
	}
}

// TestDemoBenchmarkCommand_ExitsNonZeroOnFailure proves the verdict reaches the
// process boundary. A scorer that prints FAILED but exits 0 lets a measurement
// script record a missed target as a pass, which is the failure mode the whole
// lane exists to prevent.
//
// It stays in go/cmd/eshu because it drives the cobra command: flag parsing and
// the error-to-exit-code mapping are the wrapper's job, not
// internal/cli/demo's.
func TestDemoBenchmarkCommand_ExitsNonZeroOnFailure(t *testing.T) {
	raw, err := json.Marshal(passingDemoEnvelope())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "demo.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"within target", []string{"--mode", "warm", "--images", "present", "--target", "10m"}, false},
		{"over target", []string{"--mode", "warm", "--images", "present", "--target", "1s"}, true},
		{"mislabelled cold", []string{"--mode", "cold", "--images", "present", "--target", "10m"}, true},
		{"bad images value", []string{"--mode", "warm", "--images", "sometimes"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newDemoBenchmarkCommand()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(append([]string{"--envelope", path}, tc.args...))

			runErr := cmd.Execute()
			if tc.wantErr && runErr == nil {
				t.Fatalf("Execute() = nil, want an error; output:\n%s", out.String())
			}
			if !tc.wantErr && runErr != nil {
				t.Fatalf("Execute() = %v, want nil; output:\n%s", runErr, out.String())
			}
		})
	}
}

// TestNewResolvedDemoRuntime_ReadsTheComposeFileOverride proves the wrapper is
// the half that touches the process environment.
//
// internal/cli/demo takes an environment lookup as a parameter, so nothing in
// that package can prove ESHU_DEMO_COMPOSE_FILE is actually consulted at
// runtime. Without this test the wrapper could pass a lookup that always
// returns "" and every package test would still pass, while an operator's
// override silently stopped working.
func TestNewResolvedDemoRuntime_ReadsTheComposeFileOverride(t *testing.T) {
	// A directory with no overlay in it or above it: without the override the
	// parent walk reaches the filesystem root and fails.
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv(demo.EnvComposeFile, "") // a developer's own export must not decide this

	if _, err := newResolvedDemoRuntime(demo.DefaultProject); err == nil {
		t.Fatal("newResolvedDemoRuntime() = nil error with no overlay anywhere; the walk is not failing, so the next case proves nothing")
	}

	t.Setenv(demo.EnvComposeFile, filepath.Join(dir, "my-overlay.yaml"))
	if _, err := newResolvedDemoRuntime(demo.DefaultProject); err != nil {
		t.Errorf("newResolvedDemoRuntime() = %v; the wrapper is not passing os.Getenv through to ResolveComposeFile", err)
	}
}

// TestResolveDemoOptions_ReadsThePortAndBindOverrides proves the wrapper passes
// os.Getenv to APIBase and MCPBase, not just to ResolveComposeFile.
//
// internal/cli/demo's own TestDemoBases_FollowTheOverlayPortOverrides hands
// APIBase a fake lookup, so it proves the function honours whatever map it is
// given and nothing about what production supplies. Replacing os.Getenv here
// with func(string) string { return "" } left every test in ./cmd/eshu/... and
// ./internal/cli/... green while `eshu demo` probed 18080/18091 instead of the
// operator's ports -- a second demo stack moved off the defaults would then
// never reach ready, and the readiness loop would time out against a healthy
// stack. Runtime keeps the resolved bases unexported, so demo.Options is the
// seam this asserts on.
func TestResolveDemoOptions_ReadsThePortAndBindOverrides(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv(demo.EnvComposeFile, filepath.Join(dir, "my-overlay.yaml"))
	t.Setenv(demo.EnvBindAddr, "10.1.2.3")
	t.Setenv(demo.EnvAPIPort, "19080")
	t.Setenv(demo.EnvMCPPort, "19091")

	opts, err := resolveDemoOptions(demo.DefaultProject)
	if err != nil {
		t.Fatalf("resolveDemoOptions() = %v, want nil", err)
	}
	if want := "http://10.1.2.3:19080"; opts.APIBase != want {
		t.Errorf("opts.APIBase = %q, want %q; the wrapper is not passing os.Getenv through to demo.APIBase",
			opts.APIBase, want)
	}
	if want := "http://10.1.2.3:19091"; opts.MCPBase != want {
		t.Errorf("opts.MCPBase = %q, want %q; the wrapper is not passing os.Getenv through to demo.MCPBase",
			opts.MCPBase, want)
	}
}
