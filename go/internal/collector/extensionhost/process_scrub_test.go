// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProcessRunnerScrubsHostCredentialEnv proves the satellite child never
// sees host credential material: names in ScrubEnv are removed from the
// inherited environment, everything else passes through, and explicit Env
// entries still apply. See eshu-hq/eshu#9256 (epic #9182 AC1: no private key
// in satellite env — presence, not use).
func TestProcessRunnerScrubsHostCredentialEnv(t *testing.T) {
	if os.Getenv("ESHU_EXTENSIONHOST_PROCESS_HELPER") == "scrub" {
		processRunnerScrubHelper(t)
		os.Exit(0)
	}
	// No t.Parallel: t.Setenv forbids it.

	const (
		secretName = "ESHU_TEST_SCRUB_SECRET_KEY"
		passName   = "ESHU_TEST_SCRUB_PASSTHROUGH"
	)
	t.Setenv(secretName, "super-secret-value")
	t.Setenv(passName, "visible-value")

	dump := filepath.Join(t.TempDir(), "env.dump")
	runner := ProcessRunner{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestProcessRunnerScrubsHostCredentialEnv"},
		Env: []string{
			"ESHU_EXTENSIONHOST_PROCESS_HELPER=scrub",
			"ESHU_TEST_SCRUB_DUMP=" + dump,
		},
		ScrubEnv: []string{secretName, "  ", ""},
	}

	if _, err := runner.RunCollector(context.Background(), Request{}); err != nil {
		t.Fatalf("RunCollector() error = %v, want nil", err)
	}
	names := dumpedEnvNames(t, dump)
	if _, leaked := names[secretName]; leaked {
		t.Fatalf("child env contains %s, want it scrubbed", secretName)
	}
	if names[passName] != "visible-value" {
		t.Fatalf("child env %s = %q, want passthrough %q", passName, names[passName], "visible-value")
	}
	if names["ESHU_EXTENSIONHOST_PROCESS_HELPER"] != "scrub" {
		t.Fatal("child env misses the explicit Env entry, want explicit Env applied after scrubbing")
	}
}

// TestProcessRunnerExplicitEnvWinsOverScrub proves operator intent wins: an
// explicit Env entry re-adds a scrubbed name, so scrubbing only withholds the
// inherited value instead of banning the name.
func TestProcessRunnerExplicitEnvWinsOverScrub(t *testing.T) {
	if os.Getenv("ESHU_EXTENSIONHOST_PROCESS_HELPER") == "scrub" {
		processRunnerScrubHelper(t)
		os.Exit(0)
	}
	// No t.Parallel: t.Setenv forbids it.

	const name = "ESHU_TEST_SCRUB_OVERRIDDEN"
	t.Setenv(name, "inherited-value")

	dump := filepath.Join(t.TempDir(), "env.dump")
	runner := ProcessRunner{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestProcessRunnerExplicitEnvWinsOverScrub"},
		Env: []string{
			"ESHU_EXTENSIONHOST_PROCESS_HELPER=scrub",
			"ESHU_TEST_SCRUB_DUMP=" + dump,
			name + "=operator-value",
		},
		ScrubEnv: []string{name},
	}

	if _, err := runner.RunCollector(context.Background(), Request{}); err != nil {
		t.Fatalf("RunCollector() error = %v, want nil", err)
	}
	raw, err := os.ReadFile(dump)
	if err != nil {
		t.Fatalf("ReadFile(dump) error = %v, want nil", err)
	}
	var values []string
	for _, line := range strings.Split(string(raw), "\n") {
		if rest, ok := strings.CutPrefix(line, name+"="); ok {
			values = append(values, rest)
		}
	}
	if len(values) != 1 || values[0] != "operator-value" {
		t.Fatalf("child env %s entries = %q, want exactly [operator-value]", name, values)
	}
}

// TestProcessRunnerPagedPathScrubsHostCredentialEnv proves the paged
// satellite path withholds host credential material too: RunPagedCollector
// builds the child environment through the same scrubbed helper. The child
// answers a legacy single-shot result, which the paged decode accepts with
// a nil page.
func TestProcessRunnerPagedPathScrubsHostCredentialEnv(t *testing.T) {
	if os.Getenv("ESHU_EXTENSIONHOST_PROCESS_HELPER") == "scrub" {
		processRunnerScrubHelper(t)
		os.Exit(0)
	}
	// No t.Parallel: t.Setenv forbids it.

	const secretName = "ESHU_TEST_SCRUB_SECRET_PAGED"
	t.Setenv(secretName, "super-secret-value")

	dump := filepath.Join(t.TempDir(), "env.dump")
	runner := ProcessRunner{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestProcessRunnerPagedPathScrubsHostCredentialEnv"},
		Env: []string{
			"ESHU_EXTENSIONHOST_PROCESS_HELPER=scrub",
			"ESHU_TEST_SCRUB_DUMP=" + dump,
		},
		ScrubEnv: []string{secretName},
	}

	if _, err := runner.RunPagedCollector(context.Background(), Request{}); err != nil {
		t.Fatalf("RunPagedCollector() error = %v, want nil", err)
	}
	names := dumpedEnvNames(t, dump)
	if _, leaked := names[secretName]; leaked {
		t.Fatalf("paged child env contains %s, want it scrubbed", secretName)
	}
}

// TestScrubbedEnvironMatchesExactNames proves scrubbing is exact: a scrubbed
// prefix must not remove longer names, and blank entries are ignored.
func TestScrubbedEnvironMatchesExactNames(t *testing.T) {
	t.Parallel()

	environ := []string{"FOO=1", "FOO2=2", "BAR=3"}
	got := scrubbedEnviron(environ, []string{"FOO", "", "  "})
	want := map[string]bool{"FOO2": true, "BAR": true}
	if len(got) != len(want) {
		t.Fatalf("scrubbedEnviron() = %q, want only FOO2 and BAR", got)
	}
	for _, entry := range got {
		name, _, _ := strings.Cut(entry, "=")
		if !want[name] {
			t.Fatalf("scrubbedEnviron() keeps %q, want only FOO2 and BAR", entry)
		}
	}
}

// processRunnerScrubHelper dumps the child environment to the file named by
// ESHU_TEST_SCRUB_DUMP, then answers the minimal valid SDK result so the
// parent's RunCollector call succeeds.
func processRunnerScrubHelper(t *testing.T) {
	t.Helper()

	lines := append([]string{}, os.Environ()...)
	if err := os.WriteFile(os.Getenv("ESHU_TEST_SCRUB_DUMP"), []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatalf("WriteFile(dump) error = %v, want nil", err)
	}
	processRunnerHelper(t)
}

// dumpedEnvNames reads a helper env dump into a name-to-value map.
func dumpedEnvNames(t *testing.T, path string) map[string]string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(dump) error = %v, want nil", err)
	}
	names := make(map[string]string)
	for _, line := range strings.Split(string(raw), "\n") {
		if name, value, ok := strings.Cut(line, "="); ok {
			names[name] = value
		}
	}
	return names
}
