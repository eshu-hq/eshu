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
