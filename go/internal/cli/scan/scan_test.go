// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scan

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeClient satisfies Client without any network contact. Execute never calls
// Get directly -- it goes through Runtime.FetchStatus/FetchQueryProbe -- so the
// fake only has to be a distinct, non-nil value the seams can be handed.
type fakeClient struct{ err error }

func (c fakeClient) Get(string, any) error { return c.err }

// stubRuntime returns a Runtime whose seams all succeed, so a test can override
// only the one seam it is about.
func stubRuntime() *Runtime {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	return &Runtime{
		Client:     fakeClient{},
		ServiceURL: "http://localhost:8080",
		Environ:    []string{"PATH=/usr/bin"},
		LookPath: func(string) (string, error) {
			return "/bin/eshu-bootstrap-index", nil
		},
		RunBootstrap: func(context.Context, string, []string, []string, io.Writer, io.Writer) error {
			return nil
		},
		FetchStatus: func(Client) (PipelineStatus, error) {
			return PipelineStatus{
				Health:            Health{State: "healthy"},
				GenerationHistory: GenerationHistory{Completed: 1},
			}, nil
		},
		FetchQueryProbe: func(Client) (map[string]any, error) {
			return map[string]any{"data": map[string]any{"repositories": []any{}}}, nil
		},
		Now: func() time.Time {
			now = now.Add(time.Second)
			return now
		},
		Wait: func(context.Context, time.Duration) error { return nil },
	}
}

func stubOptions(t *testing.T) Options {
	t.Helper()
	root := t.TempDir()
	return Options{
		Wait:         true,
		Timeout:      time.Minute,
		PollInterval: time.Second,
		Profile:      "local",
		ReposDir:     filepath.Join(root, "cache", "repos"),
		Target:       Target{Path: root, Root: root, Kind: "directory"},
	}
}

func TestExecuteRunsBootstrapThenReportsReady(t *testing.T) {
	rt := stubRuntime()
	var gotArgs, gotEnv []string
	rt.RunBootstrap = func(_ context.Context, binary string, args, env []string, _, _ io.Writer) error {
		if binary != "/bin/eshu-bootstrap-index" {
			t.Fatalf("binary = %q, want /bin/eshu-bootstrap-index", binary)
		}
		gotArgs = append([]string(nil), args...)
		gotEnv = append([]string(nil), env...)
		return nil
	}
	opts := stubOptions(t)

	result, err := Execute(context.Background(), io.Discard, io.Discard, *rt, opts, false)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if result.Status != "ready" {
		t.Fatalf("result.Status = %q, want ready", result.Status)
	}
	if got, want := strings.Join(gotArgs, " "), "eshu-bootstrap-index --path "+opts.Target.Root; got != want {
		t.Fatalf("bootstrap args = %q, want %q", got, want)
	}
	if got := envValue(gotEnv, "ESHU_FILESYSTEM_ROOT"); got != opts.Target.Root {
		t.Fatalf("ESHU_FILESYSTEM_ROOT = %q, want %q", got, opts.Target.Root)
	}
	if got := envValue(gotEnv, "PATH"); got != "/usr/bin" {
		t.Fatalf("PATH = %q, want the Runtime.Environ base to be inherited", got)
	}
	if result.Evidence.ServiceURL != "http://localhost:8080" {
		t.Fatalf("Evidence.ServiceURL = %q, want the runtime service URL", result.Evidence.ServiceURL)
	}
	if result.Truth["freshness"] != "current" {
		t.Fatalf("Truth[freshness] = %v, want current", result.Truth["freshness"])
	}
}

func TestExecuteReturnsPreflightFailureBeforeBootstrap(t *testing.T) {
	rt := stubRuntime()
	rt.FetchStatus = func(Client) (PipelineStatus, error) {
		return PipelineStatus{}, errors.New("connection refused")
	}
	bootstrapped := false
	rt.RunBootstrap = func(context.Context, string, []string, []string, io.Writer, io.Writer) error {
		bootstrapped = true
		return nil
	}

	_, err := Execute(context.Background(), io.Discard, io.Discard, *rt, stubOptions(t), false)
	if err == nil {
		t.Fatal("Execute() error = nil, want preflight failure")
	}
	if !strings.Contains(err.Error(), "scan preflight status check") {
		t.Fatalf("Execute() error = %q, want preflight status detail", err.Error())
	}
	if bootstrapped {
		t.Fatal("RunBootstrap called after a failed preflight")
	}
}

func TestExecuteWithoutWaitReportsSubmitted(t *testing.T) {
	rt := stubRuntime()
	opts := stubOptions(t)
	opts.Wait = false

	result, err := Execute(context.Background(), io.Discard, io.Discard, *rt, opts, false)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if result.Status != "submitted" {
		t.Fatalf("result.Status = %q, want submitted", result.Status)
	}
}

func TestExecuteAllowPartialKeepsWarningsInsteadOfFailing(t *testing.T) {
	rt := stubRuntime()
	fetches := 0
	rt.FetchStatus = func(Client) (PipelineStatus, error) {
		fetches++
		if fetches == 1 {
			return PipelineStatus{Health: Health{State: "healthy"}}, nil
		}
		return PipelineStatus{
			Health: Health{State: "degraded", Reasons: []string{"queue has dead-letter work"}},
			Queue:  Queue{DeadLetter: 1},
		}, nil
	}
	opts := stubOptions(t)
	opts.AllowPartial = true

	result, err := Execute(context.Background(), io.Discard, io.Discard, *rt, opts, false)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil under AllowPartial", err)
	}
	if result.Status != "partial" {
		t.Fatalf("result.Status = %q, want partial", result.Status)
	}
	if len(result.Warnings) == 0 || !strings.Contains(strings.Join(result.Warnings, "|"), "dead-letter") {
		t.Fatalf("result.Warnings = %v, want the readiness failure recorded", result.Warnings)
	}
}

func TestExecuteAnnouncesTargetWhenAsked(t *testing.T) {
	out := &strings.Builder{}
	opts := stubOptions(t)

	if _, err := Execute(context.Background(), out, io.Discard, *stubRuntime(), opts, true); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if want := "Scanning " + opts.Target.Root + "...\n"; out.String() != want {
		t.Fatalf("stdout = %q, want %q", out.String(), want)
	}
}

func TestExecuteRejectsRuntimeMissingAProcessSeam(t *testing.T) {
	for _, tc := range []struct {
		name   string
		break_ func(rt *Runtime)
		want   string
	}{
		{"client", func(rt *Runtime) { rt.Client = nil }, "Runtime.Client"},
		{"environ", func(rt *Runtime) { rt.Environ = nil }, "Runtime.Environ"},
		{"lookpath", func(rt *Runtime) { rt.LookPath = nil }, "Runtime.LookPath"},
		{"runbootstrap", func(rt *Runtime) { rt.RunBootstrap = nil }, "Runtime.RunBootstrap"},
		{"fetchstatus", func(rt *Runtime) { rt.FetchStatus = nil }, "Runtime.FetchStatus"},
		{"fetchqueryprobe", func(rt *Runtime) { rt.FetchQueryProbe = nil }, "Runtime.FetchQueryProbe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt := stubRuntime()
			tc.break_(rt)
			_, err := Execute(context.Background(), io.Discard, io.Discard, *rt, stubOptions(t), false)
			if err == nil {
				t.Fatalf("Execute() error = nil, want a missing-seam error for %s", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Execute() error = %q, want it to name %s", err.Error(), tc.want)
			}
		})
	}
}

func TestExecuteDefaultsTheClockSeams(t *testing.T) {
	rt := stubRuntime()
	rt.Now = nil
	rt.Wait = nil

	result, err := Execute(context.Background(), io.Discard, io.Discard, *rt, stubOptions(t), false)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil with nil Now/Wait", err)
	}
	if result.Status != "ready" {
		t.Fatalf("result.Status = %q, want ready", result.Status)
	}
}

func TestBootstrapEnvOverridesTheBaseEnvironment(t *testing.T) {
	opts := Options{
		Target:          Target{Root: "/work/root"},
		ReposDir:        "/cache/repos",
		DiscoveryReport: "/tmp/discovery.json",
	}
	base := []string{
		"PATH=/usr/bin",
		"ESHU_REPO_SOURCE_MODE=githubOrg",
		"ESHU_FILESYSTEM_DIRECT=false",
	}

	env := opts.BootstrapEnv(base)

	for key, want := range map[string]string{
		"PATH":                   "/usr/bin",
		"ESHU_REPO_SOURCE_MODE":  "filesystem",
		"ESHU_FILESYSTEM_ROOT":   "/work/root",
		"ESHU_FILESYSTEM_DIRECT": "true",
		"ESHU_REPOS_DIR":         "/cache/repos",
		"ESHU_DISCOVERY_REPORT":  "/tmp/discovery.json",
	} {
		if got := envValue(env, key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestBootstrapEnvPrefersRuntimeEnvOverTheProcessBase(t *testing.T) {
	opts := Options{
		Target:     Target{Root: "/work/root"},
		RuntimeEnv: []string{"ESHU_POSTGRES_DSN=owner-dsn"},
	}

	env := opts.BootstrapEnv([]string{"ESHU_POSTGRES_DSN=process-dsn", "PATH=/usr/bin"})

	if got := envValue(env, "ESHU_POSTGRES_DSN"); got != "owner-dsn" {
		t.Fatalf("ESHU_POSTGRES_DSN = %q, want the RuntimeEnv value", got)
	}
	if got := envValue(env, "PATH"); got != "" {
		t.Fatalf("PATH = %q, want the process base dropped when RuntimeEnv is set", got)
	}
}

func TestBootstrapArgsAddForceOnlyWhenRequested(t *testing.T) {
	opts := Options{Target: Target{Root: "/work/root"}}
	if got, want := strings.Join(opts.BootstrapArgs(), " "), "eshu-bootstrap-index --path /work/root"; got != want {
		t.Fatalf("BootstrapArgs() = %q, want %q", got, want)
	}
	opts.Force = true
	if got, want := strings.Join(opts.BootstrapArgs(), " "), "eshu-bootstrap-index --path /work/root --force"; got != want {
		t.Fatalf("BootstrapArgs(force) = %q, want %q", got, want)
	}
}

func TestResolveTargetClassifiesTheWorkspaceKind(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("Mkdir(.git) error = %v, want nil", err)
	}

	target, err := ResolveTarget(repo, "")
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v, want nil", err)
	}
	if target.Kind != "repository" {
		t.Fatalf("target.Kind = %q, want repository", target.Kind)
	}

	plain := t.TempDir()
	explicit, err := ResolveTarget(plain, plain)
	if err != nil {
		t.Fatalf("ResolveTarget(explicit) error = %v, want nil", err)
	}
	if explicit.Kind != "workspace" {
		t.Fatalf("explicit.Kind = %q, want workspace", explicit.Kind)
	}
}

func TestReposDirLandsUnderTheManagedHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ESHU_HOME", home)

	dir, err := ReposDir(t.TempDir())
	if err != nil {
		t.Fatalf("ReposDir() error = %v, want nil", err)
	}
	if !strings.HasPrefix(dir, home) {
		t.Fatalf("ReposDir() = %q, want it under ESHU_HOME %q", dir, home)
	}
	if !strings.HasSuffix(dir, filepath.Join("cache", "repos")) {
		t.Fatalf("ReposDir() = %q, want a cache/repos suffix", dir)
	}
}

func TestCurrentGraphBackendReadsTheConfiguredBackend(t *testing.T) {
	t.Setenv("ESHU_GRAPH_BACKEND", "nornicdb")
	if got := CurrentGraphBackend(); got != "nornicdb" {
		t.Fatalf("CurrentGraphBackend() = %q, want nornicdb", got)
	}
	t.Setenv("ESHU_GRAPH_BACKEND", "  ")
	if got := CurrentGraphBackend(); got != "unknown" {
		t.Fatalf("CurrentGraphBackend() = %q, want unknown for a blank value", got)
	}
}

func TestTruthCarriesTheRuntimeLevel(t *testing.T) {
	truth := Truth("current", "complete", "local", "nornicdb")
	for key, want := range map[string]any{
		"level":        "runtime",
		"freshness":    "current",
		"completeness": "complete",
		"profile":      "local",
		"backend":      "nornicdb",
	} {
		if truth[key] != want {
			t.Fatalf("Truth()[%q] = %v, want %v", key, truth[key], want)
		}
	}
}

// envValue reads key out of a KEY=VALUE environment slice.
func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}
