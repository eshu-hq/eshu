// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildVulnerabilitySuppressionsQuarantinesMissingIdentity(t *testing.T) {
	t.Parallel()

	valid := vulnerabilitySuppressionFactEnvelope(
		"suppression-valid",
		facts.VulnerabilitySuppressionSourcePolicy,
		facts.VulnerabilitySuppressionJustificationAcceptedRisk,
		"shared_token",
		"2026-07-27T12:00:00Z",
		"",
		map[string]any{"cve_id": "CVE-2026-00001"},
	)
	valid.SchemaVersion = facts.VulnerabilitySuppressionSchemaVersionV1
	malformed := valid.Clone()
	malformed.FactID = "suppression-malformed"
	delete(malformed.Payload, "suppression_id")

	suppressions, quarantined, err := BuildVulnerabilitySuppressions(
		[]facts.Envelope{malformed, valid},
	)
	if err != nil {
		t.Fatalf("BuildVulnerabilitySuppressions() error = %v, want nil", err)
	}
	if len(suppressions) != 1 || suppressions[0].SuppressionID != "suppression-valid" {
		t.Fatalf("suppressions = %#v, want only valid suppression", suppressions)
	}
	if len(quarantined) != 1 ||
		quarantined[0].factID != "suppression-malformed" ||
		quarantined[0].field != "suppression_id" {
		t.Fatalf("quarantined = %#v, want malformed suppression_id", quarantined)
	}
}

func TestBuildVulnerabilitySuppressionsQuarantinesInvalidSourceJustificationPair(t *testing.T) {
	t.Parallel()

	malformed := vulnerabilitySuppressionFactEnvelope(
		"suppression-malformed-pair",
		facts.VulnerabilitySuppressionSourceProviderDismissal,
		facts.VulnerabilitySuppressionJustificationAcceptedRisk,
		"provider:operator",
		"2026-07-27T12:00:00Z",
		"",
		map[string]any{"cve_id": "CVE-2026-00001"},
	)
	malformed.SchemaVersion = facts.VulnerabilitySuppressionSchemaVersionV1

	suppressions, quarantined, err := BuildVulnerabilitySuppressions(
		[]facts.Envelope{malformed},
	)
	if err != nil {
		t.Fatalf("BuildVulnerabilitySuppressions() error = %v, want nil", err)
	}
	if len(suppressions) != 0 {
		t.Fatalf("suppressions = %#v, want none", suppressions)
	}
	if len(quarantined) != 1 ||
		quarantined[0].factID != "suppression-malformed-pair" ||
		quarantined[0].field != "justification" {
		t.Fatalf("quarantined = %#v, want invalid source/justification pair", quarantined)
	}
}

func TestBuildVulnerabilitySuppressionsQuarantinesUnknownSourceAsSource(t *testing.T) {
	t.Parallel()

	malformed := vulnerabilitySuppressionFactEnvelope(
		"suppression-unknown-source",
		"external_unknown",
		facts.VulnerabilitySuppressionJustificationAcceptedRisk,
		"provider:operator",
		"2026-07-27T12:00:00Z",
		"",
		map[string]any{"cve_id": "CVE-2026-00001"},
	)
	malformed.SchemaVersion = facts.VulnerabilitySuppressionSchemaVersionV1

	suppressions, quarantined, err := BuildVulnerabilitySuppressions([]facts.Envelope{malformed})
	if err != nil {
		t.Fatalf("BuildVulnerabilitySuppressions() error = %v, want nil", err)
	}
	if len(suppressions) != 0 {
		t.Fatalf("suppressions = %#v, want none", suppressions)
	}
	if len(quarantined) != 1 ||
		quarantined[0].factID != "suppression-unknown-source" ||
		quarantined[0].field != "source" {
		t.Fatalf("quarantined = %#v, want unknown source field", quarantined)
	}
}

func TestBuildVulnerabilitySuppressionsQuarantinesIgnoredWithoutExpiry(t *testing.T) {
	t.Parallel()

	malformed := vulnerabilitySuppressionFactEnvelope(
		"suppression-ignored-without-expiry",
		facts.VulnerabilitySuppressionSourcePolicy,
		facts.VulnerabilitySuppressionJustificationIgnored,
		"shared_token",
		"2026-07-27T12:00:00Z",
		"",
		map[string]any{"cve_id": "CVE-2026-00001"},
	)

	suppressions, quarantined, err := BuildVulnerabilitySuppressions([]facts.Envelope{malformed})
	if err != nil {
		t.Fatalf("BuildVulnerabilitySuppressions() error = %v, want nil", err)
	}
	if len(suppressions) != 0 {
		t.Fatalf("suppressions = %#v, want none", suppressions)
	}
	if len(quarantined) != 1 ||
		quarantined[0].factID != "suppression-ignored-without-expiry" ||
		quarantined[0].field != "expires_at" {
		t.Fatalf("quarantined = %#v, want missing expires_at", quarantined)
	}
}

func TestBuildVulnerabilitySuppressionsQuarantinesEvidencePathWithoutIdentityAnchor(t *testing.T) {
	t.Parallel()

	malformed := vulnerabilitySuppressionFactEnvelope(
		"suppression-evidence-path-only",
		facts.VulnerabilitySuppressionSourcePolicy,
		facts.VulnerabilitySuppressionJustificationAcceptedRisk,
		"shared_token",
		"2026-07-27T12:00:00Z",
		"",
		map[string]any{"evidence_path": []any{"vulnerability.cve"}},
	)

	suppressions, quarantined, err := BuildVulnerabilitySuppressions([]facts.Envelope{malformed})
	if err != nil {
		t.Fatalf("BuildVulnerabilitySuppressions() error = %v, want nil", err)
	}
	if len(suppressions) != 0 {
		t.Fatalf("suppressions = %#v, want none", suppressions)
	}
	if len(quarantined) != 1 ||
		quarantined[0].factID != "suppression-evidence-path-only" ||
		quarantined[0].field != "scope" {
		t.Fatalf("quarantined = %#v, want invalid scope", quarantined)
	}
}

func TestBuildVulnerabilitySuppressionsQuarantinesDeploymentContextWithoutIdentityAnchor(t *testing.T) {
	t.Parallel()

	for _, scope := range []map[string]any{
		{"environment": "prod"},
		{"workload_id": "workload:example-api"},
		{"service_id": "service:example-api"},
		{
			"environment": "prod",
			"workload_id": "workload:example-api",
			"service_id":  "service:example-api",
		},
	} {
		malformed := vulnerabilitySuppressionFactEnvelope(
			"suppression-deployment-only",
			facts.VulnerabilitySuppressionSourcePolicy,
			facts.VulnerabilitySuppressionJustificationAcceptedRisk,
			"shared_token",
			"2026-07-27T12:00:00Z",
			"",
			scope,
		)

		suppressions, quarantined, err := BuildVulnerabilitySuppressions([]facts.Envelope{malformed})
		if err != nil {
			t.Fatalf("BuildVulnerabilitySuppressions() error = %v, want nil", err)
		}
		if len(suppressions) != 0 {
			t.Fatalf("scope %#v: suppressions = %#v, want none", scope, suppressions)
		}
		if len(quarantined) != 1 ||
			quarantined[0].factID != "suppression-deployment-only" ||
			quarantined[0].field != "scope" {
			t.Fatalf("scope %#v: quarantined = %#v, want invalid scope", scope, quarantined)
		}
	}
}
