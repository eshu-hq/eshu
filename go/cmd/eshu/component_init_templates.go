// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "os"

type componentInitTemplateFile struct {
	Path string
	Mode os.FileMode
	Body string
}

var componentInitCollectorFiles = []componentInitTemplateFile{
	{Path: "manifest.yaml", Mode: 0o644, Body: componentInitManifestTemplate},
	{Path: "README.md", Mode: 0o644, Body: componentInitReadmeTemplate},
	{Path: "config.example.yaml", Mode: 0o644, Body: componentInitConfigTemplate},
	{Path: "go.mod", Mode: 0o644, Body: componentInitGoModTemplate},
	{Path: "collector.go", Mode: 0o644, Body: componentInitCollectorGoTemplate},
	{Path: "collector_test.go", Mode: 0o644, Body: componentInitCollectorTestTemplate},
	{Path: "scripts/verify-local.sh", Mode: 0o755, Body: componentInitVerifyScriptTemplate},
}

const componentInitManifestTemplate = `apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: {{.ComponentID}}
  name: {{.Name}}
  publisher: {{.Publisher}}
  version: 0.1.0
spec:
  compatibleCore: ">=0.0.5 <0.2.0"
  componentType: collector
  runtime:
    sdkProtocol: collector-sdk/v1alpha1
    adapter: oci
  collectorKinds:
    - {{.CollectorKind}}
  artifacts:
    # Replace this placeholder digest with the published component artifact digest
    # before install in any shared or hosted environment.
    - platform: linux/amd64
      image: {{.ImageRef}}
  emittedFacts:
    - kind: {{.FactKind}}
      schemaVersions:
        - 1.0.0
      sourceConfidence:
        - observed
  consumerContracts:
    reducer:
      phases:
        - {{.ConsumerPhase}}
  telemetry:
    metricsPrefix: {{.MetricsPrefix}}
`

const componentInitReadmeTemplate = `# {{.Name}}

This package is a minimal Eshu collector extension scaffold for ` + "`{{.ComponentID}}`" + `.
It observes one source scope and emits ` + "`{{.FactKind}}`" + ` facts through the
public ` + "`collector-sdk/v1alpha1`" + ` boundary. It does not write graph rows,
apply core database migrations, claim workflow work directly, or shape query
answers.

## Local Verification

Run the generated package tests first:

` + "```bash" + `
go test ./...
` + "```" + `

From an Eshu source checkout, point the scaffold at the local SDK while the SDK
module is under active development:

` + "```bash" + `
ESHU_COLLECTOR_SDK_REPLACE=/path/to/eshu/sdk/go/collector ./scripts/verify-local.sh
` + "```" + `

The tests fail if the manifest fact kind, schema version, source confidence, SDK
contract, and emitted facts drift apart.

## Component Package Commands

Inspect and verify the manifest locally:

` + "```bash" + `
eshu component inspect ./manifest.yaml
eshu component verify ./manifest.yaml \
  --trust-mode allowlist \
  --allow-id {{.ComponentID}} \
  --allow-publisher {{.Publisher}}
` + "```" + `

Install and enable in an isolated component home:

` + "```bash" + `
eshu component install ./manifest.yaml \
  --component-home ./.eshu-components \
  --trust-mode allowlist \
  --allow-id {{.ComponentID}} \
  --allow-publisher {{.Publisher}}

eshu component enable {{.ComponentID}} \
  --component-home ./.eshu-components \
  --instance local-demo \
  --mode scheduled \
  --claims \
  --config ./config.example.yaml

eshu component list --component-home ./.eshu-components
eshu component disable {{.ComponentID}} \
  --component-home ./.eshu-components \
  --instance local-demo
` + "```" + `

Local install and enable prove package-manager state only. They do not prove
hosted activation, provenance verification, reducer admission, graph truth, or
query truth.

## Trust And Artifact Notes

The manifest uses an OCI image reference with a placeholder SHA256 digest. Build
and publish an artifact, then replace the digest before a shared install.
` + "`strict`" + ` trust mode requires Cosign signature and SLSA provenance checks
for the final digest-pinned artifact. Use ` + "`allowlist`" + ` only for local review
of this exact component ID and publisher.

Keep credentials out of this repository. Use environment variables, secret
handles, workload identity, or local profiles in private config files.
`

const componentInitConfigTemplate = `# Example local config for {{.ComponentID}}.
# Do not put API tokens, passwords, private hostnames, or raw source payloads in
# this file. Reference secret handles or environment variables instead.
source:
  env: {{.ExampleConfigEnv}}
  scope_id: local-demo
`

const componentInitGoModTemplate = `module {{.ModulePath}}

go 1.26.0

require github.com/eshu-hq/eshu/sdk/go/collector v0.1.0

// For local SDK development from an Eshu checkout, run:
// go mod edit -replace github.com/eshu-hq/eshu/sdk/go/collector=/path/to/eshu/sdk/go/collector
`

const componentInitCollectorGoTemplate = `package {{.PackageName}}

import (
	"time"

	"github.com/eshu-hq/eshu/sdk/go/collector"
)

const (
	// ComponentID is the component package identity declared in manifest.yaml.
	ComponentID = "{{.ComponentID}}"
	// CollectorKind is the source family this extension observes.
	CollectorKind = "{{.CollectorKind}}"

	factKind{{.FactConstSuffix}}      = "{{.FactKind}}"
	schemaVersion{{.FactConstSuffix}} = "1.0.0"
)

// Contract declares the facts this collector may emit through the SDK boundary.
func Contract() collector.Contract {
	return collector.Contract{
		ProtocolVersion: collector.ProtocolVersionV1Alpha1,
		Facts: []collector.FactDeclaration{
			{
				Kind:             factKind{{.FactConstSuffix}},
				SchemaVersions:   []string{schemaVersion{{.FactConstSuffix}}},
				SourceConfidence: []collector.SourceConfidence{collector.SourceConfidenceObserved},
			},
		},
	}
}

// CollectDemo returns one redacted sample observation for local conformance tests.
func CollectDemo(claim collector.Claim, observedAt time.Time) collector.Result {
	return collector.Result{
		ProtocolVersion: collector.ProtocolVersionV1Alpha1,
		State:           collector.ResultComplete,
		Claim:           claim,
		Generation: collector.Generation{
			ID:         claim.GenerationID,
			ObservedAt: observedAt,
		},
		Facts: []collector.Fact{
			{
				Kind:             factKind{{.FactConstSuffix}},
				SchemaVersion:    schemaVersion{{.FactConstSuffix}},
				StableKey:        "{{.ExampleRecordID}}",
				SourceConfidence: collector.SourceConfidenceObserved,
				ObservedAt:       observedAt,
				SourceRef: collector.SourceRef{
					SourceSystem: claim.SourceSystem,
					ScopeID:      claim.Scope.ID,
					GenerationID: claim.GenerationID,
					FactKey:      "{{.ExampleRecordID}}",
					URI:          "{{.ExampleSourceURI}}",
					RecordID:     "{{.ExampleRecordID}}",
				},
				Payload: map[string]any{
					"observation_id": "{{.ExampleRecordID}}",
					"state":          "observed",
				},
			},
		},
		Statuses: []collector.Status{
			{
				Class:     collector.StatusComplete,
				FactCount: 1,
			},
		},
	}
}
`

const componentInitCollectorTestTemplate = `package {{.PackageName}}

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/sdk/go/collector"
)

func TestManifestDeclaresCollectorContract(t *testing.T) {
	raw, err := os.ReadFile("manifest.yaml")
	if err != nil {
		t.Fatalf("read manifest.yaml: %v", err)
	}
	body := string(raw)
	for name, want := range map[string]string{
		"component id":       ComponentID,
		"collector kind":     CollectorKind,
		"fact kind":          factKind{{.FactConstSuffix}},
		"schema version":     schemaVersion{{.FactConstSuffix}},
		"source confidence":  string(collector.SourceConfidenceObserved),
		"sdk protocol":       collector.ProtocolVersionV1Alpha1,
		"consumer contract":  "{{.ConsumerPhase}}",
		"telemetry prefix":   "{{.MetricsPrefix}}",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("manifest missing %s %q", name, want)
		}
	}
}

func TestCollectDemoMatchesContract(t *testing.T) {
	observedAt := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	result := CollectDemo(testClaim(observedAt), observedAt)
	report, err := collector.NewValidator(Contract()).ValidateResult(result)
	if err != nil {
		t.Fatalf("ValidateResult() error = %v, want nil", err)
	}
	if report.FactCount != 1 {
		t.Fatalf("FactCount = %d, want 1", report.FactCount)
	}
}

func TestContractRejectsFactSchemaDrift(t *testing.T) {
	observedAt := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	result := CollectDemo(testClaim(observedAt), observedAt)
	result.Facts[0].SchemaVersion = "9.9.9"
	_, err := collector.NewValidator(Contract()).ValidateResult(result)
	if err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("ValidateResult() error = %v, want schema drift error", err)
	}
}

func testClaim(now time.Time) collector.Claim {
	return collector.Claim{
		ComponentID:   ComponentID,
		InstanceID:    "local-demo",
		CollectorKind: CollectorKind,
		SourceSystem:  "example",
		Scope: collector.Scope{
			ID:   "local-demo",
			Kind: "example_scope",
		},
		SourceRunID:  "run-local-demo",
		GenerationID: "generation-local-demo",
		WorkItemID:   "work-local-demo",
		FencingToken: "fence-local-demo",
		Attempt:      1,
		Deadline:     now.Add(time.Minute),
		ConfigHandle: "local-example-config",
	}
}
`

const componentInitVerifyScriptTemplate = `#!/usr/bin/env sh
set -eu

if [ -n "${ESHU_COLLECTOR_SDK_REPLACE:-}" ]; then
  go mod edit -replace "github.com/eshu-hq/eshu/sdk/go/collector=${ESHU_COLLECTOR_SDK_REPLACE}"
fi

go mod tidy
go test ./...

if command -v eshu >/dev/null 2>&1; then
  eshu component inspect ./manifest.yaml
  eshu component verify ./manifest.yaml \
    --trust-mode allowlist \
    --allow-id {{.ComponentID}} \
    --allow-publisher {{.Publisher}}
else
  printf '%s\n' 'eshu binary not found on PATH; skipped component inspect/verify.'
fi
`
