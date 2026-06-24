// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSupplyChainImpactHandlerEvaluatesSuppressionAndPersistsDecision(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 9.8),
			vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0001", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
			suppressionFactEnvelope(
				"suppression-1",
				facts.VulnerabilitySuppressionSourceVEX,
				facts.VulnerabilitySuppressionJustificationNotAffected,
				"vex:openvex/operator@example.com",
				"2026-05-10T00:00:00Z",
				"",
				map[string]any{
					"cve_id":        "CVE-2026-0001",
					"package_id":    testImpactPackageID,
					"repository_id": testImpactRepositoryID,
				},
			),
		},
		active: []facts.Envelope{
			packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{
		FactLoader: loader,
		Writer:     writer,
		Now:        func() time.Time { return time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC) },
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-impact",
		ScopeID:      "vuln-intel://osv/npm/example",
		GenerationID: "generation-impact",
		SourceSystem: "vulnerability_intelligence",
		Domain:       DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if writer.calls != 1 {
		t.Fatalf("Writer calls = %d, want 1", writer.calls)
	}
	if got, want := len(writer.write.Findings), 1; got != want {
		t.Fatalf("len(Findings) = %d, want %d", got, want)
	}
	finding := writer.write.Findings[0]
	if got, want := finding.Suppression.State, SupplyChainSuppressionStateNotAffected; got != want {
		t.Fatalf("Suppression.State = %q, want %q", got, want)
	}
	if finding.Suppression.SuppressionID != "suppression-1" {
		t.Fatalf("Suppression.SuppressionID = %q, want suppression-1", finding.Suppression.SuppressionID)
	}
	if !strings.Contains(result.EvidenceSummary, "suppression_not_affected=1") {
		t.Fatalf("EvidenceSummary = %q, want suppression count surfaced", result.EvidenceSummary)
	}
}

func TestSupplyChainImpactHandlerKeepsExpiredSuppressionVisible(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-1", "CVE-2026-0030", 7.5),
			vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0030", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
			suppressionFactEnvelope(
				"suppression-expired",
				facts.VulnerabilitySuppressionSourcePolicy,
				facts.VulnerabilitySuppressionJustificationIgnored,
				"eshu:policy/operator@example.com",
				"2026-04-01T00:00:00Z",
				"2026-05-14T00:00:00Z",
				map[string]any{
					"cve_id":        "CVE-2026-0030",
					"package_id":    testImpactPackageID,
					"repository_id": testImpactRepositoryID,
				},
			),
		},
		active: []facts.Envelope{
			packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{
		FactLoader: loader,
		Writer:     writer,
		Now:        func() time.Time { return time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC) },
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-expired",
		ScopeID:      "vuln-intel://osv/npm/example",
		GenerationID: "generation-impact",
		SourceSystem: "vulnerability_intelligence",
		Domain:       DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	}); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	finding := writer.write.Findings[0]
	if finding.Suppression.State != SupplyChainSuppressionStateExpired {
		t.Fatalf("Suppression.State = %q, want %q", finding.Suppression.State, SupplyChainSuppressionStateExpired)
	}
	if finding.Suppression.ExpiresAt.IsZero() {
		t.Fatalf("Suppression.ExpiresAt = zero, want expiration preserved")
	}
}

func TestSupplyChainImpactPayloadIncludesSuppressionState(t *testing.T) {
	t.Parallel()

	write := SupplyChainImpactWrite{
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
	}
	finding := SupplyChainImpactFinding{
		CVEID:        "CVE-2026-0001",
		PackageID:    testImpactPackageID,
		RepositoryID: testImpactRepositoryID,
		Suppression: SupplyChainSuppressionDecision{
			State:         SupplyChainSuppressionStateAcceptedRisk,
			SuppressionID: "suppression-accepted",
			Source:        facts.VulnerabilitySuppressionSourcePolicy,
			Justification: facts.VulnerabilitySuppressionJustificationAcceptedRisk,
			Author:        "eshu:policy/operator@example.com",
			AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
			Reason:        "compensating control",
		},
	}
	payload := supplyChainImpactPayload(write, finding)
	if got, want := payload["suppression_state"], string(SupplyChainSuppressionStateAcceptedRisk); got != want {
		t.Fatalf("suppression_state = %#v, want %#v", got, want)
	}
	suppression, ok := payload["suppression"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing suppression block: %#v", payload)
	}
	if got, want := suppression["state"], string(SupplyChainSuppressionStateAcceptedRisk); got != want {
		t.Fatalf("suppression.state = %#v, want %#v", got, want)
	}
	if got, want := suppression["author"], "eshu:policy/operator@example.com"; got != want {
		t.Fatalf("suppression.author = %#v, want %#v", got, want)
	}
}

func suppressionFactEnvelope(
	id string,
	source string,
	justification string,
	author string,
	authoredAt string,
	expiresAt string,
	scope map[string]any,
) facts.Envelope {
	payload := map[string]any{
		"suppression_id": id,
		"source":         source,
		"justification":  justification,
		"author":         author,
		"authored_at":    authoredAt,
		"scope":          scope,
	}
	if expiresAt != "" {
		payload["expires_at"] = expiresAt
	}
	return facts.Envelope{
		FactID:   id,
		FactKind: facts.VulnerabilitySuppressionFactKind,
		Payload:  payload,
	}
}
