// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
)

// TestAlpha2ManifestFlowsRegistryValidationThroughParseConfig proves the
// onboarding path end to end: an explicitly configured collector-sdk/v1alpha2
// manifest passes registry validation (install plus installed-manifest
// reload), and an activation naming that manifest's protocol and digest
// parses.
func TestAlpha2ManifestFlowsRegistryValidationThroughParseConfig(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "component.yaml")
	if err := os.WriteFile(manifestPath, []byte(alpha2ManifestYAML()), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v, want nil", err)
	}
	registry := component.NewRegistry(home)
	installed, err := registry.Install(manifestPath, component.VerificationResult{
		Allowed:   true,
		Mode:      component.TrustModeAllowlist,
		Component: "dev.eshu.collector.aws",
		Publisher: "eshu-hq",
		Version:   "0.1.0",
	})
	if err != nil {
		t.Fatalf("Install() error = %v, want nil for alpha2 manifest", err)
	}
	loaded, err := registry.LoadInstalledManifest(installed.ID, installed.Version)
	if err != nil {
		t.Fatalf("LoadInstalledManifest() error = %v, want nil for alpha2 manifest", err)
	}
	if got := strings.TrimSpace(loaded.Spec.Runtime.SDKProtocol); got != component.CollectorSDKProtocolV1Alpha2 {
		t.Fatalf("loaded SDKProtocol = %q, want %q", loaded.Spec.Runtime.SDKProtocol, component.CollectorSDKProtocolV1Alpha2)
	}

	raw := `{"schema_version":"eshu.component.instance.v1",` +
		`"component_id":"` + loaded.Metadata.ID + `",` +
		`"component_version":"` + loaded.Metadata.Version + `",` +
		`"manifest_digest":"` + installed.ManifestDigest + `",` +
		`"config_handle":"component-config:abcd",` +
		`"runtime":{"sdk_protocol":"` + loaded.Spec.Runtime.SDKProtocol + `","adapter":"oci"}}`
	config, ok, err := ParseConfig(raw)
	if err != nil {
		t.Fatalf("ParseConfig() error = %v, want nil for alpha2 manifest flow", err)
	}
	if !ok {
		t.Fatal("ParseConfig() ok = false, want true for alpha2 manifest flow")
	}
	if got := strings.TrimSpace(config.Runtime.SDKProtocol); got != component.CollectorSDKProtocolV1Alpha2 {
		t.Fatalf("ParseConfig() SDKProtocol = %q, want %q", config.Runtime.SDKProtocol, component.CollectorSDKProtocolV1Alpha2)
	}
	if config.ManifestDigest != installed.ManifestDigest {
		t.Fatalf("ParseConfig() ManifestDigest = %q, want %q", config.ManifestDigest, installed.ManifestDigest)
	}
}

// alpha2ManifestYAML mirrors the component package's valid manifest fixture
// with an explicitly configured collector-sdk/v1alpha2 runtime.
func alpha2ManifestYAML() string {
	return `apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: dev.eshu.collector.aws
  name: AWS cloud scanner
  publisher: eshu-hq
  version: 0.1.0
spec:
  compatibleCore: ">=0.0.5 <0.1.0"
  componentType: collector
  collectorKinds:
    - aws
  runtime:
    sdkProtocol: collector-sdk/v1alpha2
    adapter: oci
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/eshu-hq/components/aws-collector@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  emittedFacts:
    - kind: dev.eshu.aws.cloud_resource
      schemaVersions:
        - 1.0.0
      sourceConfidence:
        - reported
  consumerContracts:
    reducer:
      phases:
        - cloud_resource_uid:canonical_nodes_committed
  telemetry:
    metricsPrefix: eshu_dp_aws_
`
}
