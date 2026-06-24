// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestValidateSBOMAttestationCollectorConfigurationAcceptsConfiguredAndOCIReferrerTargets(t *testing.T) {
	t.Parallel()

	raw := `{
		"targets": [
			{
				"scope_id": "sbom://configured/example",
				"source_type": "configured_source",
				"artifact_kind": "sbom",
				"document_format": "cyclonedx",
				"document_url": "http://sbom-fixture:8080/library/example.cdx.json",
				"subject_digest": "sha256:1111111111111111111111111111111111111111111111111111111111111111"
			},
			{
				"scope_id": "sbom://oci/referrer/example",
				"source_type": "oci_referrer",
				"artifact_kind": "attestation",
				"document_format": "in_toto",
				"provider": "ghcr",
				"registry": "https://ghcr.io",
				"repository": "eshu-hq/eshu",
				"subject_digest": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				"referrer_digest": "sha256:2222222222222222222222222222222222222222222222222222222222222222"
			}
		]
	}`

	if err := ValidateSBOMAttestationCollectorConfiguration(raw); err != nil {
		t.Fatalf("ValidateSBOMAttestationCollectorConfiguration() error = %v, want nil", err)
	}
}

func TestValidateSBOMAttestationCollectorConfigurationRejectsUnsafeOrUnboundedTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			name: "configured source requires document URL",
			raw: `{"targets":[{
				"scope_id": "sbom://configured/example",
				"source_type": "configured_source",
				"artifact_kind": "sbom",
				"document_format": "cyclonedx"
			}]}`,
			wantErr: "document_url is required",
		},
		{
			name: "document URL must not carry credentials",
			raw: `{"targets":[{
				"scope_id": "sbom://configured/example",
				"source_type": "configured_source",
				"artifact_kind": "sbom",
				"document_format": "cyclonedx",
				"document_url": "https://user:secret@sbom.example.com/sbom.json"
			}]}`,
			wantErr: "document_url must not include credentials",
		},
		{
			name: "document URL must use HTTP",
			raw: `{"targets":[{
				"scope_id": "sbom://configured/example",
				"source_type": "configured_source",
				"artifact_kind": "sbom",
				"document_format": "cyclonedx",
				"document_url": "ftp://sbom.example.com/sbom.json"
			}]}`,
			wantErr: "document_url must use http or https",
		},
		{
			name: "oci referrer requires referrer digest",
			raw: `{"targets":[{
				"scope_id": "sbom://oci/referrer/example",
				"source_type": "oci_referrer",
				"artifact_kind": "attestation",
				"document_format": "in_toto",
				"provider": "ghcr",
				"registry": "https://ghcr.io",
				"repository": "eshu-hq/eshu",
				"subject_digest": "sha256:1111111111111111111111111111111111111111111111111111111111111111"
			}]}`,
			wantErr: "referrer_digest is required",
		},
		{
			name: "unknown format rejected",
			raw: `{"targets":[{
				"scope_id": "sbom://configured/example",
				"source_type": "configured_source",
				"artifact_kind": "sbom",
				"document_format": "xml",
				"document_url": "https://sbom.example.com/sbom.xml"
			}]}`,
			wantErr: `unsupported document_format "xml"`,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateSBOMAttestationCollectorConfiguration(tc.raw)
			if err == nil {
				t.Fatalf("ValidateSBOMAttestationCollectorConfiguration() error = nil, want %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateSBOMAttestationCollectorConfiguration() error = %q, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestDesiredCollectorInstanceValidatesSBOMAttestationConfiguration(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "sbom-attestation",
		CollectorKind: scope.CollectorSBOMAttestation,
		Mode:          CollectorModeScheduled,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{"targets":[{
			"scope_id": "sbom://configured/example",
			"source_type": "configured_source",
			"artifact_kind": "sbom",
			"document_format": "cyclonedx",
			"document_url": "https://sbom.example.com/sbom.json"
		}]}`,
	}
	if err := instance.Validate(); err != nil {
		t.Fatalf("DesiredCollectorInstance.Validate() error = %v, want nil", err)
	}

	materialized := instance.Materialize(time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC))
	if err := materialized.Validate(); err != nil {
		t.Fatalf("CollectorInstance.Validate() error = %v, want nil", err)
	}
}
