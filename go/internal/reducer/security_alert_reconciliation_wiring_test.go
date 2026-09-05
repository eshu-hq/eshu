// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// TestSecurityAlertReconciliationRegistrationCarriesTheManifestConsumptionSeam
// guards the wiring, not the logic.
//
// extractSecurityAlertManifestConsumptions is the reducer root's
// manifest/lockfile bridge (security_alert_manifest_dependency_match.go). On
// the pre-#6061 tree the securityalert build called it unconditionally; it is
// now an injected field, and
// securityalert.BuildSecurityAlertReconciliationsWithQuarantine skips it when
// nil so the family's in-package unit tests can build without importing the
// reducer root. That nil-tolerance means a dropped registration line ships
// inert: every securityalert logic test stays green while production silently
// downgrades every lockfile-only alert from "matched" to "provider_only" with
// no error, no counter, and no deferral (the pending-impact defer keys on
// DependencyEvidenceID, which is exactly what the dropped bridge populates).
//
// This test therefore does two things a logic test cannot: it asserts the
// registered handler carries the seam at all, and it proves the seam is the
// real bridge by running one lockfile-only alert end to end through the
// registered handler and demanding the manifest-derived dependency evidence.
func TestSecurityAlertReconciliationRegistrationCarriesTheManifestConsumptionSeam(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/acme/api"
	packageID := "npm://registry.npmjs.org/wiring-lib"
	loader := &recordingSecurityAlertReconciliationFactLoader{
		scopeFacts: []facts.Envelope{
			securityAlertEnvelope("alert-wiring", repoID, map[string]any{
				"provider":              "github_dependabot",
				"provider_alert_number": int64(21),
				"provider_state":        "open",
				"package_id":            packageID,
				"package_name":          "wiring-lib",
				"ecosystem":             "npm",
				"manifest_path":         "package-lock.json",
				"cve_ids":               []string{"CVE-2026-9600"},
				"ghsa_ids":              []string{"GHSA-synthetic-9600"},
			}),
		},
		activeFacts: []facts.Envelope{
			supplyChainImpactFindingEnvelope(
				"impact-wiring",
				repoID,
				packageID,
				"CVE-2026-9600",
				"affected_exact",
			),
		},
		// The ONLY consumption evidence in this fixture is manifest/lockfile
		// evidence: there is no reducer_package_consumption_correlation fact,
		// so the decision can only reach "matched" through the injected
		// bridge.
		manifestFacts: []facts.Envelope{
			packageManifestDependencyFactWithMetadata(
				repoID,
				"api",
				"package-lock.json",
				"wiring-lib",
				"npm",
				"1.2.3",
				time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC),
				map[string]any{
					"section":           "package-lock",
					"lockfile":          true,
					"dependency_path":   []any{"wiring-lib"},
					"dependency_depth":  1,
					"direct_dependency": true,
				},
			),
		},
	}
	writer := &recordingSecurityAlertReconciliationWriter{}

	definitions := appendSupplyChainCorrelationAdditiveDomains(nil, DefaultHandlers{
		FactLoader: loader,
		SupplyChainSecurityHandlers: SupplyChainSecurityHandlers{
			SecurityAlertReconciliationWriter: writer,
		},
	})

	var handler securityalert.SecurityAlertReconciliationHandler
	found := false
	for _, definition := range definitions {
		if definition.Domain != DomainSecurityAlertReconciliation {
			continue
		}
		typed, ok := definition.Handler.(securityalert.SecurityAlertReconciliationHandler)
		if !ok {
			t.Fatalf(
				"handler for %s = %T, want securityalert.SecurityAlertReconciliationHandler",
				definition.Domain,
				definition.Handler,
			)
		}
		handler, found = typed, true
	}
	if !found {
		t.Fatalf("no %s registration found", DomainSecurityAlertReconciliation)
	}
	if handler.ExtractManifestConsumptions == nil {
		t.Fatal(
			"ExtractManifestConsumptions is nil on the registered handler: " +
				"manifest/lockfile consumption evidence would ship inert and every " +
				"lockfile-only alert would silently reconcile as provider_only",
		)
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-security-alert-wiring",
		ScopeID:      repoID,
		GenerationID: "security-alert-generation-1",
		SourceSystem: "security_alert",
		Domain:       DomainSecurityAlertReconciliation,
		Cause:        "provider alert observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Handle() status = %q, want succeeded", result.Status)
	}
	if writer.calls != 1 {
		t.Fatalf("writer calls = %d, want 1", writer.calls)
	}
	if len(writer.write.Decisions) != 1 {
		t.Fatalf("decisions = %d, want 1", len(writer.write.Decisions))
	}
	decision := writer.write.Decisions[0]
	if got, want := decision.Status, securityalert.SecurityAlertReconciliationMatched; got != want {
		t.Fatalf(
			"decision status = %q, want %q; reason=%q: the registration wired no "+
				"manifest-consumption bridge, so the lockfile evidence was invisible",
			got, want, decision.Reason,
		)
	}
	// Prove it is the real bridge, not merely some non-nil function: only
	// extractSecurityAlertManifestConsumptions mints a manifest-dep dependency
	// evidence id from the package_manifest_dependency fact.
	if got, want := decision.DependencyEvidenceID, "manifest-dep:"+repoID+":wiring-lib"; got != want {
		t.Fatalf("decision DependencyEvidenceID = %q, want manifest-derived %q", got, want)
	}
}
