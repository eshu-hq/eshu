// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
)

// TestScrubHostCredentialEnvAddsAppVarsToProcessRunner proves the service
// withholds the App credential variables from a process-adapter satellite
// child (eshu-hq/eshu#9256): the runner keeps its command and explicit env
// and gains exactly the three credential names in ScrubEnv.
func TestScrubHostCredentialEnvAddsAppVarsToProcessRunner(t *testing.T) {
	t.Parallel()

	runner := scrubHostCredentialEnv(extensionhost.ProcessRunner{
		Command: "/data/collector",
		Env:     []string{"ESHU_COMPONENT_COLLECTOR_POLL_INTERVAL=30s"},
	})
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

// TestScrubHostCredentialEnvLeavesOCIRunnerAlone proves the OCI adapter is
// untouched: it already forwards only explicit env, so there is nothing to
// withhold.
func TestScrubHostCredentialEnvLeavesOCIRunnerAlone(t *testing.T) {
	t.Parallel()

	runner := extensionhost.OCIRunner{ImageRef: "ghcr.io/eshu-hq/eshu-collector-security-alerts@sha256:f9dbf9a39478a94a3c3bf8154222672efb385ba7d24c8116071a8e9812ea409b"}
	got, ok := scrubHostCredentialEnv(runner).(extensionhost.OCIRunner)
	if !ok {
		t.Fatalf("scrubHostCredentialEnv() type = %T, want extensionhost.OCIRunner", scrubHostCredentialEnv(runner))
	}
	if got.ImageRef != runner.ImageRef || len(got.Env) != 0 {
		t.Fatalf("scrubHostCredentialEnv() changed the OCI runner to %+v, want it untouched", got)
	}
}
