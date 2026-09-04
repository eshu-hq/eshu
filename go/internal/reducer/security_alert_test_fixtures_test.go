// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// securityAlertEnvelope and packageConsumptionCorrelationEnvelope are
// declared locally rather than imported from securityalert's test package:
// securityalert.security_alert_reconciliation_test.go's identically-named
// fixture builders are package-private test helpers (issue #6061), and
// supply_chain_impact's own tests here (provider-alert seeding, manifest
// dependency scoping, input-invalid quarantine) still need equivalent
// security_alert.repository_alert and package-consumption-correlation
// envelope fixtures. Bodies unchanged from before the securityalert family
// move.
func securityAlertEnvelope(factID string, repoID string, payload map[string]any) facts.Envelope {
	payload["repository_id"] = repoID
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          repoID,
		GenerationID:     "generation-1",
		FactKind:         facts.SecurityAlertRepositoryAlertFactKind,
		SchemaVersion:    facts.SecurityAlertSchemaVersionV1,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC),
		Payload:          payload,
	}
}

func packageConsumptionCorrelationEnvelope(factID string, repoID string, packageID string, relativePath string) facts.Envelope {
	return facts.Envelope{
		FactID:       factID,
		ScopeID:      repoID,
		GenerationID: "generation-1",
		FactKind:     packageConsumptionCorrelationFactKind,
		ObservedAt:   time.Date(2026, 5, 23, 11, 0, 0, 0, time.UTC),
		Payload: map[string]any{
			"repository_id": repoID,
			"package_id":    packageID,
			"relative_path": relativePath,
			"outcome":       "exact",
		},
	}
}

// supplyChainImpactFindingEnvelope is declared locally for the same reason as
// securityAlertEnvelope above.
func supplyChainImpactFindingEnvelope(
	factID string,
	repoID string,
	packageID string,
	cveID string,
	impactStatus string,
) facts.Envelope {
	return facts.Envelope{
		FactID:       factID,
		ScopeID:      repoID,
		GenerationID: "generation-1",
		FactKind:     supplyChainImpactFactKind,
		ObservedAt:   time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC),
		Payload: map[string]any{
			"repository_id": repoID,
			"package_id":    packageID,
			"cve_id":        cveID,
			"advisory_id":   "GHSA-abcd-1234",
			"impact_status": impactStatus,
		},
	}
}

// securityAlertEnvelopeMissingRepositoryID builds a
// security_alert.repository_alert envelope whose payload deliberately omits
// the required repository_id identity anchor, so the typed decode seam
// dead-letters it as input_invalid. It intentionally does NOT route through
// securityAlertEnvelope, which always stamps repository_id.
func securityAlertEnvelopeMissingRepositoryID(factID string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          "security-alert:github:acme/api",
		GenerationID:     "generation-1",
		FactKind:         facts.SecurityAlertRepositoryAlertFactKind,
		SchemaVersion:    facts.SecurityAlertSchemaVersionV1,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC),
		Payload:          payload,
	}
}
