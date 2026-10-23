// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/component"
)

const grantedCommandManifest = `apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: dev.example.collector.alpha
  name: Alpha collector
  publisher: eshu-hq
  version: 0.1.0
spec:
  compatibleCore: ">=0.0.5 <0.2.0"
  componentType: collector
  collectorKinds:
    - aws
  runtime:
    sdkProtocol: collector-sdk/v1alpha1
    adapter: oci
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/eshu-hq/components/alpha-collector@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  emittedFacts:
    - kind: aws_resource
      schemaVersions:
        - 1.0.0
      sourceConfidence:
        - reported
  consumerContracts:
    reducer:
      phases:
        - cloud_resource_uid:canonical_nodes_committed
  telemetry:
    metricsPrefix: eshu_dp_alpha_
`

func writeGrantedCommandManifest(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "component.yaml")
	if err := os.WriteFile(path, []byte(grantedCommandManifest), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v, want nil", err)
	}
	return path
}

func recordGrantedCommandGrant(t *testing.T, home string) {
	t.Helper()

	err := component.NewRegistry(home).RecordGrant(component.ProducerGrant{
		ProducerID:     "dev.example.collector.alpha",
		Version:        "0.1.0",
		Kind:           "aws_resource",
		SchemaVersions: []string{"1.0.0"},
		Scope:          "aws",
		ExpiresAt:      time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("RecordGrant() error = %v, want nil", err)
	}
}

func newGrantedCommand(out *bytes.Buffer, home string) *cobra.Command {
	cmd := newComponentTestCommand(out)
	cmd.Flags().String(componentHomeFlag, home, "")
	cmd.Flags().String(componentTrustModeFlag, component.TrustModeAllowlist, "")
	cmd.Flags().StringSlice(componentAllowIDFlag, []string{"dev.example.collector.alpha"}, "")
	cmd.Flags().StringSlice(componentAllowPublisherFlag, []string{"eshu-hq"}, "")
	return cmd
}

// TestComponentInspectAndVerifyHonorProducerGrantsInHome proves issue #8909:
// inspect and verify judge a manifest that declares a granted core-owned fact
// kind against the producer grants recorded in --component-home.
func TestComponentInspectAndVerifyHonorProducerGrantsInHome(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	recordGrantedCommandGrant(t, home)
	manifestPath := writeGrantedCommandManifest(t)

	inspectOut := &bytes.Buffer{}
	if err := runComponentInspect(newGrantedCommand(inspectOut, home), []string{manifestPath}); err != nil {
		t.Fatalf("runComponentInspect() error = %v, want nil", err)
	}
	if !strings.Contains(inspectOut.String(), "dev.example.collector.alpha") {
		t.Fatalf("inspect output = %q, want the component identity", inspectOut.String())
	}

	verifyOut := &bytes.Buffer{}
	if err := runComponentVerify(newGrantedCommand(verifyOut, home), []string{manifestPath}); err != nil {
		t.Fatalf("runComponentVerify() error = %v, want nil", err)
	}
	if !strings.Contains(verifyOut.String(), "verified dev.example.collector.alpha@0.1.0 with allowlist policy") {
		t.Fatalf("verify output = %q, want the verified line", verifyOut.String())
	}
}

// TestComponentInspectAndVerifyFailClosedWithoutGrants proves the same
// manifest keeps failing with the core-owned error when the home holds no
// matching grant.
func TestComponentInspectAndVerifyFailClosedWithoutGrants(t *testing.T) {
	t.Parallel()

	manifestPath := writeGrantedCommandManifest(t)
	for name, run := range map[string]func(*cobra.Command, []string) error{
		"inspect": runComponentInspect,
		"verify":  runComponentVerify,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := run(newGrantedCommand(&bytes.Buffer{}, t.TempDir()), []string{manifestPath})
			if err == nil {
				t.Fatalf("%s error = nil, want the core-owned fail-closed error", name)
			}
			if !strings.Contains(err.Error(), "is core-owned by Eshu and cannot be claimed by optional components") {
				t.Fatalf("%s error = %v, want the core-owned error", name, err)
			}
		})
	}
}
