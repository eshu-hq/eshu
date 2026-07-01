// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSupplyChainImpactHandlerSubDurationsAndSignals(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 9.8),
			vulnerabilityAffectedPackageFact(
				"affected-1",
				"CVE-2026-0001",
				testImpactPackageID,
				"npm",
				"example",
				"1.2.3",
				"1.3.0",
			),
			packageConsumptionFactWithRange(
				"consume-1",
				testImpactPackageID,
				testImpactRepositoryID,
				"1.2.3",
			),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-impact",
		ScopeID:      "package-registry:npm:example",
		GenerationID: "generation-package",
		SourceSystem: "package_registry",
		Domain:       DomainSupplyChainImpact,
		Cause:        "package registry identity observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	assertSubDurationsPresent(t, result, "supply_chain_impact", []string{
		"load_scope_facts",
		"load_repository_facts",
		"load_manifest_dependencies",
		"load_active_evidence",
		"load_python_reachability",
		"load_jvm_reachability",
		"security_alert_scoping",
		"build_findings",
		"evaluate_suppressions",
		"write_findings",
		"emit_counters",
		"total",
	})
	assertInputReady(t, result, "supply_chain_impact", 1)
	assertWrittenRows(t, result, "supply_chain_impact", 1)
	for _, key := range []string{
		"scope_facts",
		"repository_facts",
		"manifest_dependency_facts",
		"active_evidence_facts",
		"python_reachability_facts",
		"jvm_reachability_facts",
		"post_scope_facts",
		"security_alert_scoping_applied",
		"security_alert_scoped_out_facts",
		"findings",
		"active_evidence_truncated",
	} {
		if _, ok := result.SubSignals[key]; !ok {
			t.Fatalf("supply_chain_impact: SubSignals missing %q; got keys: %v", key, mapKeys(result.SubSignals))
		}
	}
}

func TestSupplyChainImpactHandlerTimesSecurityAlertScoping(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 9.8),
			vulnerabilityAffectedPackageFact(
				"affected-1",
				"CVE-2026-0001",
				testImpactPackageID,
				"npm",
				"example",
				"1.2.3",
				"1.3.0",
			),
			securityAlertRepositoryAlertImpactFact(
				"alert-1",
				testImpactRepositoryID,
				testImpactPackageID,
				"CVE-2026-0001",
			),
			packageConsumptionFactWithRange(
				"consume-1",
				testImpactPackageID,
				testImpactRepositoryID,
				"1.2.3",
			),
			packageConsumptionFactWithRange(
				"consume-other",
				testImpactPackageID,
				"repository:other",
				"1.2.3",
			),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-alert-impact",
		ScopeID:      "security-alerts:repo",
		GenerationID: "generation-alert",
		SourceSystem: "security_alert",
		Domain:       DomainSupplyChainImpact,
		Cause:        "provider alert observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	if got := len(writer.write.Findings); got != 1 {
		t.Fatalf("len(Findings) = %d, want 1", got)
	}
	assertSubDurationsPresent(t, result, "supply_chain_impact", []string{
		"security_alert_scoping",
	})
	for key, want := range map[string]float64{
		"post_scope_facts":                4,
		"security_alert_scoping_applied":  1,
		"security_alert_scoped_out_facts": 1,
	} {
		if got := result.SubSignals[key]; got != want {
			t.Fatalf("SubSignals[%q] = %v, want %v", key, got, want)
		}
	}
}
