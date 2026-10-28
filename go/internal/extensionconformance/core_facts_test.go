// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionconformance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu-sdk/factschema/fixturepack"
)

const awsCoreManifest = `apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: dev.example.collector.aws
  name: AWS collector
  publisher: example
  version: 0.1.0
spec:
  compatibleCore: ">=0.0.5 <0.2.0"
  componentType: collector
  collectorKinds:
    - aws
  runtime:
    sdkProtocol: collector-sdk/v1alpha1
    adapter: process
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/example/aws@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  emittedFacts:
    - kind: aws_resource
      payloadSchemaRef: aws_resource
      schemaVersions:
        - 1.0.0
      sourceConfidence:
        - reported
  consumerContracts:
    reducer:
      phases:
        - aws_cloud_runtime_drift
  telemetry:
    metricsPrefix: eshu_dp_example_aws_
`

func writeAWSCoreManifest(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(path, []byte(awsCoreManifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

func awsCoreResult(t *testing.T) sdkcollector.Result {
	t.Helper()
	result := validConformanceResult()
	result.Claim.ComponentID = "dev.example.collector.aws"
	result.Claim.CollectorKind = "aws"
	result.Claim.SourceSystem = result.Claim.ComponentID
	result.Facts[0].Kind = "aws_resource"
	result.Facts[0].SourceRef.SourceSystem = result.Claim.ComponentID
	payload, ok := fixturepack.ValidPayload("aws_resource")
	if !ok {
		t.Fatal("aws_resource fixture pack payload missing")
	}
	result.Facts[0].Payload = payload
	return result
}

// TestRunAcceptsGrantedAWSResource proves a built host can pass a core-owned
// fixture through the SDK's trusted authorization context.
func TestRunAcceptsGrantedAWSResource(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	grant := liveGrant()
	grant.ProducerID = "dev.example.collector.aws"
	grant.Kind = "aws_resource"
	grant.Scope = "aws"
	recordGrants(t, home, grant)
	report, err := Run(context.Background(), Request{
		ManifestPath:  writeAWSCoreManifest(t),
		FixturePaths:  []string{writeConformanceFixture(t, awsCoreResult(t))},
		Mode:          ModeFixture,
		ComponentHome: home,
	})
	if err != nil || report.Status != StatusPassed {
		t.Fatalf("Run() = status %q, error %v, findings %#v; want passed", report.Status, err, report.Findings)
	}
}

// TestRunRejectsCrossGrantTuple proves separate grants cannot be combined into
// permission to emit a schema version from an unauthorized collector kind.
func TestRunRejectsCrossGrantTuple(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	docsGrant := liveGrant()
	docsGrant.ProducerID = "dev.example.collector.docs"
	docsGrant.Kind = "documentation_section"
	docsGrant.Scope = "docs"
	otherGrant := docsGrant
	otherGrant.Scope = "other"
	otherGrant.SchemaVersions = []string{"1.1.0"}
	recordGrants(t, home, docsGrant, otherGrant)
	manifest := strings.ReplaceAll(awsCoreManifest, "aws_resource", "documentation_section")
	manifest = strings.ReplaceAll(manifest, "dev.example.collector.aws", "dev.example.collector.docs")
	manifest = strings.Replace(manifest, "aws_cloud_runtime_drift", "documentation_materialization", 1)
	manifest = strings.Replace(manifest, "    - aws\n", "    - docs\n    - other\n", 1)
	manifest = strings.Replace(manifest, "        - 1.0.0\n", "        - 1.0.0\n        - 1.1.0\n", 1)
	manifestPath := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	result := awsCoreResult(t)
	result.Claim.ComponentID = "dev.example.collector.docs"
	result.Claim.CollectorKind = "docs"
	result.Claim.SourceSystem = result.Claim.ComponentID
	result.Facts[0].SourceRef.SourceSystem = result.Claim.ComponentID
	result.Facts[0].SchemaVersion = "1.1.0"
	result.Facts[0].Kind = "documentation_section"
	payload, ok := fixturepack.ValidPayload("documentation_section")
	if !ok {
		t.Fatal("documentation_section fixture pack payload missing")
	}
	result.Facts[0].Payload = payload
	report, err := Run(context.Background(), Request{
		ManifestPath:  manifestPath,
		FixturePaths:  []string{writeConformanceFixture(t, result)},
		Mode:          ModeFixture,
		ComponentHome: home,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want exact-tuple denial")
	}
	assertFinding(t, report, FindingFixtureContractFailed, true, true)
	if !strings.Contains(report.Findings[0].Message, "lacks exact collector/schema authorization") {
		t.Fatalf("finding = %q, want exact-tuple denial", report.Findings[0].Message)
	}
}

// TestRunRejectsConflictingCoreSchema proves manifest metadata cannot replace
// the core registry's authoritative payload schema for a granted kind.
func TestRunRejectsConflictingCoreSchema(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	grant := liveGrant()
	grant.ProducerID = "dev.example.collector.aws"
	grant.Kind = "aws_resource"
	grant.Scope = "aws"
	recordGrants(t, home, grant)
	manifest := strings.Replace(awsCoreManifest, "payloadSchemaRef: aws_resource", "payloadSchemaRef: aws_warning", 1)
	manifestPath := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	report, err := Run(context.Background(), Request{
		ManifestPath:  manifestPath,
		FixturePaths:  []string{writeConformanceFixture(t, awsCoreResult(t))},
		Mode:          ModeFixture,
		ComponentHome: home,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want trusted schema conflict")
	}
	assertFinding(t, report, FindingPayloadSchemaInvalid, true, true)
	if !strings.Contains(report.Findings[0].Message, "payloadSchemaRef conflicts with manifest") {
		t.Fatalf("finding = %q, want trusted schema conflict", report.Findings[0].Message)
	}
}

// TestRunRejectsGrantedUnsupportedCoreVersion proves a live grant cannot make
// an unsupported core schema version pass fixture conformance.
func TestRunRejectsGrantedUnsupportedCoreVersion(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	grant := liveGrant()
	grant.ProducerID = "dev.example.collector.aws"
	grant.Kind = "aws_resource"
	grant.Scope = "aws"
	grant.SchemaVersions = []string{"2.0.0"}
	recordGrants(t, home, grant)
	manifest := strings.Replace(awsCoreManifest, "        - 1.0.0\n", "        - 2.0.0\n", 1)
	manifestPath := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	result := awsCoreResult(t)
	result.Facts[0].SchemaVersion = "2.0.0"
	report, err := Run(context.Background(), Request{
		ManifestPath:  manifestPath,
		FixturePaths:  []string{writeConformanceFixture(t, result)},
		Mode:          ModeFixture,
		ComponentHome: home,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want unsupported core schema version")
	}
	assertFinding(t, report, FindingManifestInvalid, true, true)
	if !strings.Contains(report.Findings[0].Message, "schema_version \"2.0.0\" is unsupported") {
		t.Fatalf("finding = %q, want unsupported core schema version", report.Findings[0].Message)
	}
}
