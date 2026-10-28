// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace/noop"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestBuildClaimedServiceScrubsAppCredentialsFromSatelliteEnv is the
// wired-path proof for the satellite-env scrub (eshu-hq/eshu#9258, epic
// #9182): it builds the service via buildClaimedService with the process
// adapter, drives one claim through the built source into a fixture process
// that dumps its child environment, and asserts the three App credential
// names are absent while an unrelated passthrough entry survives. Reverting
// the scrub wiring in buildClaimedService must turn this test red: the unit
// seam tests alone stay green without the wiring (false-green-proof class).
func TestBuildClaimedServiceScrubsAppCredentialsFromSatelliteEnv(t *testing.T) {
	if os.Getenv("ESHU_COMPONENT_WIRED_PROOF_HELPER") == "dump" {
		wiredProofHelper(t)
		os.Exit(0)
	}
	// No t.Parallel: t.Setenv forbids it.

	// The child inherits the real process environment, so the credential
	// names must be set here, not only in the mapEnv below. Sentinel values
	// carry no real secret material. The provider stays nil on purpose: the
	// mapEnv below omits the App variables, so no network mint fires before
	// launch, while the scrub is unconditional and covers exactly this
	// partial-configuration shape too.
	t.Setenv(envGitHubAppID, "wired-proof-app-id")
	t.Setenv(envGitHubAppPrivateKey, "wired-proof-private-key")
	t.Setenv(envGitHubAppInstallations, `{"wired-proof-scope":"111"}`)
	const passthroughName = "ESHU_WIRED_PROOF_PASSTHROUGH"
	t.Setenv(passthroughName, "visible-value")

	dump := filepath.Join(t.TempDir(), "env.dump")
	componentHome := t.TempDir()
	installEnabledComponent(t, componentHome, component.RuntimeAdapterProcess, writeWiredProofConfig(t, dump))

	service, err := buildClaimedService(
		&fakeExecQueryer{},
		mapEnv(map[string]string{
			envComponentHome:            componentHome,
			envComponentTrustMode:       component.TrustModeAllowlist,
			envComponentAllowIDs:        "dev.eshu.examples.scorecard",
			envComponentAllowPublishers: "eshu-hq",
			envComponentCoreVersion:     "dev",
			envCollectorOwnerID:         "component-owner-a",
		}),
		noop.NewTracerProvider().Tracer("test"),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}
	source, ok := service.Source.(*extensionhost.Source)
	if !ok {
		t.Fatalf("Source = %T, want *extensionhost.Source", service.Source)
	}

	if _, ok, err := source.NextClaimed(context.Background(), wiredProofWorkItem()); err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	} else if !ok {
		t.Fatal("NextClaimed() ok = false, want true for the complete helper result")
	}

	names := wiredProofDumpedEnvNames(t, dump)
	for _, name := range []string{envGitHubAppID, envGitHubAppPrivateKey, envGitHubAppInstallations} {
		if value, leaked := names[name]; leaked {
			t.Fatalf("satellite child env contains %s=%q, want it scrubbed by the buildClaimedService wiring", name, value)
		}
	}
	if names[passthroughName] != "visible-value" {
		t.Fatalf("satellite child env %s = %q, want passthrough %q", passthroughName, names[passthroughName], "visible-value")
	}
}

// writeWiredProofConfig points the fixture activation at the test binary
// itself: the child re-runs this test with the dump helper set, dumps its
// environment to dump, and answers a minimal valid SDK result.
func writeWiredProofConfig(t *testing.T, dump string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := `host:
  sourceSystem: openssf-scorecard
  scope:
    id: github.com/example/widgets
    kind: repository
process:
  command: ` + os.Args[0] + `
  args:
    - -test.run=TestBuildClaimedServiceScrubsAppCredentialsFromSatelliteEnv
  env:
    - ESHU_COMPONENT_WIRED_PROOF_HELPER=dump
    - ESHU_WIRED_PROOF_DUMP=` + dump + `
config:
  source:
    input: /fixtures/scorecard.json
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write wired-proof config: %v", err)
	}
	return path
}

// wiredProofWorkItem mirrors the installed fixture activation: kind
// scorecard, instance scorecard-local, claimed with a live lease.
func wiredProofWorkItem() workflow.WorkItem {
	now := time.Now().UTC()
	return workflow.WorkItem{
		WorkItemID:          "work-wired-proof-1",
		RunID:               "run-wired-proof",
		CollectorKind:       scope.CollectorKind("scorecard"),
		CollectorInstanceID: "scorecard-local",
		SourceSystem:        "git",
		ScopeID:             "repo-eshu",
		TenantID:            "tenant-1",
		WorkspaceID:         "workspace-1",
		SubjectClass:        "repository",
		PolicyRevisionHash:  "policy-hash",
		AcceptanceUnitID:    "acceptance-unit",
		SourceRunID:         "generation-1",
		GenerationID:        "generation-1",
		FairnessKey:         "git:repo-eshu",
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentClaimID:      "claim-wired-proof-1",
		CurrentFencingToken: 7,
		CurrentOwnerID:      "worker-1",
		LeaseExpiresAt:      now.Add(2 * time.Minute),
		VisibleAt:           now.Add(-time.Minute),
		LastClaimedAt:       now.Add(-time.Second),
		CreatedAt:           now.Add(-10 * time.Minute),
		UpdatedAt:           now.Add(-time.Second),
	}
}

// wiredProofHelper dumps the satellite child environment, then echoes the
// host claim back as a minimal valid complete result: one declared fact kind
// with a complete status covering it.
func wiredProofHelper(t *testing.T) {
	t.Helper()

	dump := os.Getenv("ESHU_WIRED_PROOF_DUMP")
	lines := append([]string{}, os.Environ()...)
	if err := os.WriteFile(dump, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatalf("WriteFile(dump) error = %v, want nil", err)
	}
	var request extensionhost.Request
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		t.Fatalf("Decode(stdin) error = %v, want nil", err)
	}
	now := time.Now().UTC()
	result := sdkcollector.Result{
		ProtocolVersion: request.ProtocolVersion,
		State:           sdkcollector.ResultComplete,
		Claim:           request.Claim,
		Generation: sdkcollector.Generation{
			ID:            request.Claim.GenerationID,
			ObservedAt:    now,
			FreshnessHint: "snapshot",
		},
		Facts: []sdkcollector.Fact{{
			Kind:             "dev.eshu.examples.scorecard.snapshot",
			SchemaVersion:    "1.0.0",
			StableKey:        "scorecard:repo-eshu",
			SourceConfidence: sdkcollector.SourceConfidenceReported,
			ObservedAt:       now,
			SourceRef: sdkcollector.SourceRef{
				SourceSystem: request.Claim.SourceSystem,
				ScopeID:      request.Claim.Scope.ID,
				GenerationID: request.Claim.GenerationID,
				FactKey:      "scorecard:repo-eshu",
				URI:          "git://repo-eshu",
				RecordID:     "scorecard-record",
			},
			Payload: map[string]any{
				"score": 98,
				"state": "pass",
			},
		}},
		Statuses: []sdkcollector.Status{{
			Class:     sdkcollector.StatusComplete,
			FactCount: 1,
		}},
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		t.Fatalf("Encode(stdout) error = %v, want nil", err)
	}
}

// wiredProofDumpedEnvNames reads a helper env dump into a name-to-value map.
func wiredProofDumpedEnvNames(t *testing.T, path string) map[string]string {
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
