// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestComponentCommandTreeIncludesIndexVerify(t *testing.T) {
	cmd, _, err := componentCmd.Find([]string{"index", "verify", "index.yaml"})
	if err != nil {
		t.Fatalf("component index verify lookup error = %v, want nil", err)
	}
	if cmd == nil || cmd.Name() != "verify" {
		t.Fatalf("component index verify command = %#v, want verify subcommand", cmd)
	}
	if cmd.Flags().Lookup(componentJSONFlag) == nil {
		t.Fatalf("component index verify missing --%s flag", componentJSONFlag)
	}
}

func TestComponentIndexVerifyAcceptsYAMLIndex(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	cmd := newComponentIndexVerifyCommand(out, false)
	path := writeComponentIndexFile(t, validComponentIndexYAML())

	if err := runComponentIndexVerify(cmd, []string{path}); err != nil {
		t.Fatalf("runComponentIndexVerify() error = %v, want nil", err)
	}
	if !strings.Contains(out.String(), "verified component index with 1 entries") {
		t.Fatalf("component index verify output = %q, want verified summary", out.String())
	}
}

func TestComponentIndexVerifyRendersJSONReport(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	cmd := newComponentIndexVerifyCommand(out, true)
	path := writeComponentIndexFile(t, validComponentIndexYAML())

	if err := runComponentIndexVerify(cmd, []string{path}); err != nil {
		t.Fatalf("runComponentIndexVerify() error = %v, want nil", err)
	}
	payload := assertComponentJSON(t, out, "index verify", "verified")
	report := payloadMap(t, payload, "index_verification")
	if got, want := report["valid"], true; got != want {
		t.Fatalf("index_verification.valid = %#v, want %#v", got, want)
	}
}

func TestComponentIndexVerifyRejectsCompatibilityIssues(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	cmd := newComponentIndexVerifyCommand(out, true)
	path := writeComponentIndexFile(t, strings.Replace(
		validComponentIndexYAML(),
		"          - 1.0.0",
		"          - one",
		1,
	))

	err := runComponentIndexVerify(cmd, []string{path})
	if err == nil {
		t.Fatal("runComponentIndexVerify() error = nil, want verifier failure")
	}
	payload := assertComponentJSON(t, out, "index verify", "failed")
	report := payloadMap(t, payload, "index_verification")
	if got, want := report["valid"], false; got != want {
		t.Fatalf("index_verification.valid = %#v, want %#v", got, want)
	}
	issues, ok := report["issues"].([]any)
	if !ok || len(issues) != 1 {
		t.Fatalf("index_verification.issues = %#v, want one issue", report["issues"])
	}
	issue, ok := issues[0].(map[string]any)
	if !ok {
		t.Fatalf("issue = %#v, want object", issues[0])
	}
	if got, want := issue["code"], "unsupported_schema_version"; got != want {
		t.Fatalf("issue.code = %#v, want %q", got, want)
	}
	if strings.Contains(out.String(), path) {
		t.Fatalf("json output leaks index path %q: %s", path, out.String())
	}
}

func newComponentIndexVerifyCommand(out *bytes.Buffer, jsonOutput bool) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	cmd.Flags().Bool(componentJSONFlag, jsonOutput, "")
	return cmd
}

func writeComponentIndexFile(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "community-extension-index.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v, want nil", err)
	}
	return path
}

func validComponentIndexYAML() string {
	return `apiVersion: eshu.dev/community-extension-index/v1alpha1
kind: CommunityExtensionIndex
entries:
  - componentId: dev.example.collector.scorecard
    publisher: example
    version: 0.1.0
    lifecycleChannel: community-maintained
    installable: true
    manifestDigest: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
    artifacts:
      - image: ghcr.io/example/scorecard@sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789
    compatibleCore: ">=0.0.5 <0.2.0"
    componentType: collector
    collectorKinds:
      - scorecard
    emittedFacts:
      - kind: dev.example.scorecard.snapshot
        schemaVersions:
          - 1.0.0
        sourceConfidence:
          - reported
    consumerContracts:
      reducer:
        phases:
          - source_evidence_only:no_graph_truth
    telemetry:
      metricsPrefix: eshu_dp_example_scorecard_
    source:
      repository: https://github.com/example/eshu-scorecard-collector
    review:
      pr: https://github.com/eshu-hq/eshu/pull/1234
    provenance:
      required: true
      mode: sigstore
      signature: sigstore:sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789
    conformance:
      schemaVersion: eshu.extension.conformance.v1
      status: passed
      proofUri: https://github.com/example/eshu-scorecard-collector/actions/runs/1234567890
    publication:
      status: published
    compatibilityBadge:
      manifestApiVersion: eshu.dev/v1alpha1
      manifestDigest: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
      compatibleCore: ">=0.0.5 <0.2.0"
      artifactDigest: sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789
      signatureStatus: signed
      provenanceStatus: verified
      runtimeProtocol: collector-sdk/v1alpha1
      adapter: process
      conformanceProofUri: https://github.com/example/eshu-scorecard-collector/actions/runs/1234567890
      conformanceStatus: passed
      policyResult: installable
`
}
