// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
)

// inheritingRunner stands in for a future extensionhost.Runner that inherits
// the host environment without scrubbing. It must never pass the scrub guard
// silently (eshu-hq/eshu#9413, epic #9182).
type inheritingRunner struct{}

func (inheritingRunner) RunCollector(context.Context, extensionhost.Request) (sdkcollector.Result, error) {
	return sdkcollector.Result{}, errors.New("inheritingRunner must not run")
}

// TestScrubHostCredentialEnvRefusesUnknownRunner proves the guard fails
// closed: a runner type the scrub does not know is refused at startup instead
// of passing through with the host environment inherited.
func TestScrubHostCredentialEnvRefusesUnknownRunner(t *testing.T) {
	t.Parallel()

	for name, runner := range map[string]extensionhost.Runner{
		"future inheriting type": inheritingRunner{},
		"nil runner":             nil,
	} {
		if _, err := scrubHostCredentialEnv(runner); err == nil {
			t.Fatalf("scrubHostCredentialEnv(%s) error = nil, want startup refusal", name)
		}
	}
	if _, err := scrubHostCredentialEnv(inheritingRunner{}); err != nil {
		if !strings.Contains(err.Error(), "inheritingRunner") {
			t.Fatalf("scrubHostCredentialEnv() error = %q, want it to name the refused type", err)
		}
	}
}

// TestScrubHostCredentialEnvAddsAppVarsToProcessRunner proves the service
// withholds the App credential variables from a process-adapter satellite
// child (eshu-hq/eshu#9256): the runner keeps its command and explicit env
// and gains exactly the three credential names in ScrubEnv.
func TestScrubHostCredentialEnvAddsAppVarsToProcessRunner(t *testing.T) {
	t.Parallel()

	runner, err := scrubHostCredentialEnv(extensionhost.ProcessRunner{
		Command: "/data/collector",
		Env:     []string{"ESHU_COMPONENT_COLLECTOR_POLL_INTERVAL=30s"},
	})
	if err != nil {
		t.Fatalf("scrubHostCredentialEnv() error = %v, want nil", err)
	}
	process, ok := runner.(extensionhost.ProcessRunner)
	if !ok {
		t.Fatalf("scrubHostCredentialEnv() type = %T, want extensionhost.ProcessRunner", runner)
	}
	if process.Command != "/data/collector" {
		t.Fatalf("scrubHostCredentialEnv() command = %q, want it preserved", process.Command)
	}
	for _, name := range []string{envGitHubAppID, envGitHubAppPrivateKey, envGitHubAppInstallations} {
		found := false
		for _, scrubbed := range process.ScrubEnv {
			if scrubbed == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("scrubHostCredentialEnv() ScrubEnv = %q, want it to contain %s", process.ScrubEnv, name)
		}
	}
}

// TestScrubHostCredentialEnvScrubsPointerRunner proves a *ProcessRunner is
// scrubbed in place: RunCollector has a value receiver, so the pointer also
// satisfies extensionhost.Runner, and silently skipping it would leak host
// credentials with a green suite (eshu-hq/eshu#9394 review).
func TestScrubHostCredentialEnvScrubsPointerRunner(t *testing.T) {
	t.Parallel()

	runner, err := scrubHostCredentialEnv(&extensionhost.ProcessRunner{Command: "/data/collector"})
	if err != nil {
		t.Fatalf("scrubHostCredentialEnv() error = %v, want nil", err)
	}
	process, ok := runner.(*extensionhost.ProcessRunner)
	if !ok {
		t.Fatalf("scrubHostCredentialEnv() type = %T, want *extensionhost.ProcessRunner", runner)
	}
	for _, name := range []string{envGitHubAppID, envGitHubAppPrivateKey, envGitHubAppInstallations} {
		found := false
		for _, scrubbed := range process.ScrubEnv {
			if scrubbed == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("scrubHostCredentialEnv() ScrubEnv = %q, want it to contain %s", process.ScrubEnv, name)
		}
	}
}

// TestScrubHostCredentialEnvLeavesOCIRunnerAlone proves the OCI adapter is
// untouched in both its value and pointer shapes: it already forwards only
// explicit env, so there is nothing to withhold (eshu-hq/eshu#9413 keeps this
// passthrough explicit while unknown types fail closed).
func TestScrubHostCredentialEnvLeavesOCIRunnerAlone(t *testing.T) {
	t.Parallel()

	const imageRef = "ghcr.io/eshu-hq/eshu-collector-security-alerts@sha256:f9dbf9a39478a94a3c3bf8154222672efb385ba7d24c8116071a8e9812ea409b"
	runners := map[string]extensionhost.Runner{
		"value":   extensionhost.OCIRunner{ImageRef: imageRef},
		"pointer": &extensionhost.OCIRunner{ImageRef: imageRef},
	}
	for name, runner := range runners {
		got, err := scrubHostCredentialEnv(runner)
		if err != nil {
			t.Fatalf("scrubHostCredentialEnv(%s) error = %v, want nil passthrough", name, err)
		}
		// The guard returns the same runner value untouched; compare by
		// shape and image rather than ==, since OCIRunner holds slices.
		switch want := runner.(type) {
		case extensionhost.OCIRunner:
			passthrough, ok := got.(extensionhost.OCIRunner)
			if !ok || passthrough.ImageRef != want.ImageRef || len(passthrough.Env) != 0 {
				t.Fatalf("scrubHostCredentialEnv(%s) = %+v, want the OCI runner untouched", name, got)
			}
		case *extensionhost.OCIRunner:
			passthrough, ok := got.(*extensionhost.OCIRunner)
			if !ok || passthrough != want {
				t.Fatalf("scrubHostCredentialEnv(%s) = %+v, want the OCI runner untouched", name, got)
			}
		default:
			t.Fatalf("test runner %s has unexpected type %T", name, runner)
		}
	}
}

// TestScrubHostCredentialEnvRefusesNilProcessRunner proves a nil process
// pointer is refused rather than panicking on the scrub append.
func TestScrubHostCredentialEnvRefusesNilProcessRunner(t *testing.T) {
	t.Parallel()

	if _, err := scrubHostCredentialEnv((*extensionhost.ProcessRunner)(nil)); err == nil {
		t.Fatal("scrubHostCredentialEnv(nil process runner) error = nil, want startup refusal")
	}
}
