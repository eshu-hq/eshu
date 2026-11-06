// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// refusedBaseReconciliationFixture builds one provider alert whose package
// pin lives only in the delta's base full generation (#10193). The stub
// loader's manifest facts are what ListActivePackageManifestDependencyFacts
// returns for the alert's package name: on a refused base
// (union_base_missing / union_scope_mismatch) that read counts the refusal,
// logs the "manifest dependency read" WARN, and returns the delta's rows
// only, so the base-only pin is absent; after the base heals, the same read
// returns the base pin. An impact finding for the alert's advisory exists in
// both cases, so owned dependency evidence is the only missing half.
func refusedBaseReconciliationFixture(manifestFacts []facts.Envelope) (*recordingSecurityAlertReconciliationFactLoader, string) {
	repoID := "repo://github/acme/api"
	packageID := "npm://registry.npmjs.org/base-only-lib"
	loader := &recordingSecurityAlertReconciliationFactLoader{
		scopeFacts: []facts.Envelope{
			securityAlertEnvelope("alert-base-only", repoID, map[string]any{
				"provider":              "github_dependabot",
				"provider_alert_number": int64(93),
				"provider_state":        "open",
				"package_id":            packageID,
				"package_name":          "base-only-lib",
				"ecosystem":             "npm",
				"manifest_path":         "package-lock.json",
				"cve_ids":               []string{"CVE-2026-10193"},
				"ghsa_ids":              []string{"GHSA-synthetic-10193"},
				"vulnerable_range":      "<1.0.1",
				"patched_version":       "1.0.1",
			}),
		},
		activeFacts: []facts.Envelope{
			supplyChainImpactFindingEnvelope("impact-base-only", repoID, packageID, "CVE-2026-10193", "affected_exact"),
		},
		manifestFacts: manifestFacts,
	}
	return loader, repoID
}

func refusedBaseManifestPin(repoID, name, version string) facts.Envelope {
	return packageManifestDependencyFactWithMetadata(
		repoID,
		"api",
		"package-lock.json",
		name,
		"npm",
		version,
		time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC),
		map[string]any{
			"section":           "package-lock",
			"lockfile":          true,
			"dependency_path":   []any{"root", name},
			"dependency_depth":  2,
			"direct_dependency": false,
		},
	)
}

// TestSecurityAlertReconciliationHandlerRefusedBaseCommitsProviderOnly is
// the #10193 acceptance test for the chosen leave-as-is option. The trigger
// inventory was confirmed against the wiring before choosing: only
// provider-alert and package-registry-package facts create
// security_alert_reconciliation intents
// (projector/security/alert_reconciliation_intents.go, and the handler's own
// trigger kinds), so no re-trigger fires when a refused base heals, and
// neither a whole-intent failure nor an impact-evidence wait can see the
// refusal (the default manifest read returns delta-only rows and no
// refusal signal; provider_only never waits). The documented behavior is
// therefore: the alert commits provider_only immediately, upsert-only with
// CanonicalWrites=0, and an operator re-drives it after heal (runbook:
// docs/public/operate/graph-delta-union.md). The healed-base control below
// proves the same alert re-derives to matched once the base pin is visible
// again, which is what the re-drive relies on.
func TestSecurityAlertReconciliationHandlerRefusedBaseCommitsProviderOnly(t *testing.T) {
	t.Parallel()

	// Refused base: the delta carried only its changed pin (delta-lib), so
	// the base-only-lib pin that lives only in the base is missing.
	loader, repoID := refusedBaseReconciliationFixture([]facts.Envelope{
		refusedBaseManifestPin("repo://github/acme/api", "delta-lib", "9.9.9"),
	})
	db := &fakeWorkloadIdentityExecer{}
	handler := securityalert.SecurityAlertReconciliationHandler{
		FactLoader:                  loader,
		Writer:                      securityalert.PostgresSecurityAlertReconciliationWriter{DB: db},
		ExtractManifestConsumptions: correlation.ExtractSecurityAlertManifestConsumptions,
	}

	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-refused-base-reconciliation",
		ScopeID:      repoID,
		GenerationID: "security-alert-generation-1",
		SourceSystem: "security_alert",
		Domain:       DomainSecurityAlertReconciliation,
		Cause:        "provider security alert evidence observed",
		AttemptCount: 1,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil: provider_only does not wait or fail on a refused base (#10193 leave-as-is)", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Handle() status = %q, want succeeded", result.Status)
	}
	if got := result.CanonicalWrites; got != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 (upsert-only writer, no graph write)", got)
	}
	if !strings.Contains(result.EvidenceSummary, "provider_only=1") {
		t.Fatalf("EvidenceSummary = %q, want provider_only=1 (the joining signal on the success line)", result.EvidenceSummary)
	}
	if got, want := strings.Join(loader.manifestEcosystems, ","), "npm"; got != want {
		t.Fatalf("manifest ecosystems = %q, want %q", got, want)
	}
	if got, want := strings.Join(loader.manifestPackageNames, ","), "base-only-lib"; got != want {
		t.Fatalf("manifest package names = %q, want %q", got, want)
	}

	// Writer assertion on the resulting rows: exactly one upserted
	// reconciliation fact, no DELETE/retract statement, provider_only with
	// the missing-owned-dependency reason and no impact_wait_outcome.
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("ExecContext calls = %d, want %d (one batched upsert)", got, want)
	}
	if strings.Contains(strings.ToUpper(db.execs[0].query), "DELETE") {
		t.Fatalf("writer query contains DELETE, want upsert-only: %s", db.execs[0].query)
	}
	rows := decodeBatchedFactCalls(t, db.execs)
	if got, want := len(rows), 1; got != want {
		t.Fatalf("decoded rows = %d, want %d (single-alert blast radius)", got, want)
	}
	if got, want := rows[0].FactKind, "reducer_security_alert_reconciliation"; got != want {
		t.Fatalf("row FactKind = %q, want %q", got, want)
	}
	var payload map[string]any
	if err := json.Unmarshal(rows[0].Payload, &payload); err != nil {
		t.Fatalf("decode row payload: %v", err)
	}
	// The #10193 residual, pinned: the base pin exists but the refused
	// base hid it, so the committed status is provider_only, not matched.
	// The healed-base control proves the same alert matches once the pin
	// is visible again.
	if got, want := payload["reconciliation_status"], string(securityalert.SecurityAlertReconciliationProviderOnly); got != want {
		t.Fatalf("reconciliation_status = %v, want %q on a refused base (#10193 leave-as-is)", got, want)
	}
	if got, _ := payload["reason"].(string); !strings.Contains(got, "no matching owned dependency evidence") {
		t.Fatalf("reason = %q, want the no-matching-owned-dependency-evidence reason", got)
	}
	if got := payload["canonical_writes"]; got != float64(0) {
		t.Fatalf("canonical_writes = %v, want 0", got)
	}
	if got, ok := payload["impact_wait_outcome"]; ok {
		t.Fatalf("provider_only row carries impact_wait_outcome %v; it never waited on impact evidence", got)
	}
	if got, _ := payload["dependency_evidence_id"].(string); got != "" {
		t.Fatalf("dependency_evidence_id = %q, want empty on a refused base", got)
	}
}

// TestSecurityAlertReconciliationHandlerHealedBaseRederivesMatched is the
// control for the refused-base test: with the base pin visible again (the
// state after the refused scope's next full generation), a later
// provider-alert trigger re-derives the same alert to matched through the
// same handler and writer. This is the re-drive the #10193 runbook relies
// on; no trigger fires on heal alone.
func TestSecurityAlertReconciliationHandlerHealedBaseRederivesMatched(t *testing.T) {
	t.Parallel()

	loader, repoID := refusedBaseReconciliationFixture([]facts.Envelope{
		refusedBaseManifestPin("repo://github/acme/api", "base-only-lib", "1.0.0"),
	})
	db := &fakeWorkloadIdentityExecer{}
	handler := securityalert.SecurityAlertReconciliationHandler{
		FactLoader:                  loader,
		Writer:                      securityalert.PostgresSecurityAlertReconciliationWriter{DB: db},
		ExtractManifestConsumptions: correlation.ExtractSecurityAlertManifestConsumptions,
	}

	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-healed-base-reconciliation",
		ScopeID:      repoID,
		GenerationID: "security-alert-generation-2",
		SourceSystem: "security_alert",
		Domain:       DomainSecurityAlertReconciliation,
		Cause:        "provider security alert evidence observed",
		AttemptCount: 1,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if !strings.Contains(result.EvidenceSummary, "matched=1") {
		t.Fatalf("EvidenceSummary = %q, want matched=1 after heal", result.EvidenceSummary)
	}
	rows := decodeBatchedFactCalls(t, db.execs)
	if got, want := len(rows), 1; got != want {
		t.Fatalf("decoded rows = %d, want %d", got, want)
	}
	var payload map[string]any
	if err := json.Unmarshal(rows[0].Payload, &payload); err != nil {
		t.Fatalf("decode row payload: %v", err)
	}
	if got, want := payload["reconciliation_status"], string(securityalert.SecurityAlertReconciliationMatched); got != want {
		t.Fatalf("reconciliation_status = %v, want %q after heal; reason=%v", got, want, payload["reason"])
	}
	if got, _ := payload["dependency_evidence_id"].(string); got == "" {
		t.Fatalf("dependency_evidence_id empty after heal, want the base pin fact; payload=%s", rows[0].Payload)
	}
}
