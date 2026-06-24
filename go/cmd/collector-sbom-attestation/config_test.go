// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/sbomruntime"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestLoadClaimedRuntimeConfigSelectsSBOMAttestationInstance(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"collector-sbom-attestation",
			"collector_kind":"sbom_attestation",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{"targets":[{
				"scope_id":"sbom://oci/referrer/example",
				"source_type":"oci_referrer",
				"artifact_kind":"sbom",
				"document_format":"cyclonedx",
				"provider":"ghcr",
				"registry":"https://ghcr.io",
				"repository":"eshu-hq/eshu",
				"subject_digest":"sha256:1111111111111111111111111111111111111111111111111111111111111111",
				"referrer_digest":"sha256:2222222222222222222222222222222222222222222222222222222222222222",
				"bearer_token_env":"REGISTRY_TOKEN"
			}]}
		}]`,
		"REGISTRY_TOKEN": "token-123",
	}

	config, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if got, want := config.Instance.CollectorKind, scope.CollectorSBOMAttestation; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	target := config.Source.Targets[0]
	if got, want := target.SourceType, sbomruntime.SourceTypeOCIReferrer; got != want {
		t.Fatalf("SourceType = %q, want %q", got, want)
	}
	if got, want := target.BearerToken, "token-123"; got != want {
		t.Fatalf("BearerToken = %q, want %q", got, want)
	}
	if strings.Contains(target.SourceURI, "token-123") {
		t.Fatalf("SourceURI = %q, want no credential material", target.SourceURI)
	}
}

func TestLoadClaimedRuntimeConfigRejectsDisabledClaims(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"collector-sbom-attestation",
			"collector_kind":"sbom_attestation",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":false,
			"configuration":{"targets":[{
				"scope_id":"sbom://configured/example",
				"source_type":"configured_source",
				"artifact_kind":"sbom",
				"document_format":"cyclonedx",
				"document_url":"https://sbom.example.com/sbom.json"
			}]}
		}]`,
	}

	_, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] })
	if err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want claim-enabled rejection")
	}
	if got := err.Error(); !strings.Contains(got, "claim-enabled") {
		t.Fatalf("loadClaimedRuntimeConfig() error = %q, want claim-enabled rejection", got)
	}
}
