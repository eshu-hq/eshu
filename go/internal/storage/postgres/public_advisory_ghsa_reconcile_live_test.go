// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9959 disposable PostgreSQL proof that reconcile_public_advisories_v1 refuses every ghsa target with the original unsupported-source error and no rows while osv still registers, and that a config the workflow ghsa validator accepts is refused through the store; isolated bootstrapped schema per test
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// publicAdvisoryProofInstance renders one desired vulnerability-intelligence
// instance whose single configured target is also its one public advisory
// declaration, in the shape reconcile_public_advisories_v1 reads.
func publicAdvisoryProofInstance(instanceID string, target map[string]any) map[string]any {
	return map[string]any{
		"instance_id": instanceID, "collector_kind": "vulnerability_intelligence",
		"mode": "continuous", "enabled": true, "claims_enabled": true, "bootstrap": false,
		"display_name": instanceID,
		"configuration": map[string]any{
			"targets": []any{target},
			"public_advisory_targets": []any{
				map[string]any{"scope_id": target["scope_id"], "max_age_seconds": 3600},
			},
		},
	}
}

func publicAdvisoryProofGHSATarget(advisoryType, scopeID string) map[string]any {
	target := map[string]any{
		"source": "ghsa", "scope_id": scopeID,
		"modified_start": "2026-05-01T00:00:00Z", "modified_end": "2026-05-02T00:00:00Z",
		"results_per_page": 100, "max_requests": 10,
	}
	if advisoryType != "" {
		target["advisory_type"] = advisoryType
	}
	return target
}

// TestPublicAdvisoryReconcileRefusesGHSALive proves the built function keeps
// refusing a ghsa target of every shape, reviewed and malware included, with the
// original unsupported-source error and no row, and that osv still registers.
// #10037 decided a ghsa scope is never public-advisory registered.
func TestPublicAdvisoryReconcileRefusesGHSALive(t *testing.T) {
	database, _ := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	principals := &scannerProofPrincipals{admin: database}
	principals.producer = mustCreateScannerProofRole(t, database, ctx, "producer")
	principals.coordinator = mustCreateScannerProofRole(t, database, ctx, "coordinator")
	principals.worker = mustCreateScannerProofRole(t, database, ctx, "worker")
	mustGrantScannerProofPrivileges(t, database, ctx, principals.producer, "typed-source-producer")
	mustGrantScannerProofPrivileges(t, database, ctx, principals.coordinator, "authority-control")
	mustGrantScannerProofPrivileges(t, database, ctx, principals.worker, "runtime-writer")
	mustSeedScannerProofPolicy(t, database, ctx, principals)

	reconcile := func(expected, desired int64, instances ...map[string]any) error {
		encoded, err := json.Marshal(instances)
		if err != nil {
			t.Fatalf("marshal instances: %v", err)
		}
		_, err = principals.coordinator.db.ExecContext(ctx, reconcilePublicAdvisoriesQuery,
			expected, desired, string(encoded), scannerProofPolicyRevision, scannerProofDatabaseIdentity)
		return err
	}
	registrationCount := func() int {
		var count int
		if err := database.QueryRowContext(ctx,
			`SELECT count(*) FROM scanner_advisory_public_registrations`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}

	refused := []struct {
		name   string
		target map[string]any
	}{
		{"ghsa reviewed", publicAdvisoryProofGHSATarget("reviewed", "vuln-intel://ghsa/reviewed")},
		{"ghsa malware", publicAdvisoryProofGHSATarget("malware", "vuln-intel://ghsa/malware")},
		{"ghsa unreviewed", publicAdvisoryProofGHSATarget("unreviewed", "vuln-intel://ghsa/unreviewed")},
		{"typeless ghsa", publicAdvisoryProofGHSATarget("", "vuln-intel://ghsa/reviewed")},
		{"glad", map[string]any{"source": "glad", "scope_id": "vuln-intel://glad/npm"}},
	}
	for _, tc := range refused {
		err := reconcile(0, 1, publicAdvisoryProofInstance("refused-"+strings.ReplaceAll(tc.name, " ", "-"), tc.target))
		if err == nil || !strings.Contains(err.Error(), publicAdvisoryUnsupportedText) {
			t.Fatalf("%s: error = %v, want %q", tc.name, err, publicAdvisoryUnsupportedText)
		}
		if got := registrationCount(); got != 0 {
			t.Fatalf("%s: %d registrations after a refused reconcile, want 0", tc.name, got)
		}
	}

	osv := publicAdvisoryProofInstance("osv-regression", map[string]any{
		"source": "osv", "scope_id": "vuln-intel://osv/npm", "ecosystem": "npm",
	})
	if err := reconcile(0, 1, osv); err != nil {
		t.Fatalf("reconcile with osv error = %v, want nil", err)
	}
	var class, kinds string
	if err := database.QueryRowContext(ctx, `SELECT trusted_source_class, allowed_fact_kinds::text
		FROM scanner_advisory_public_registrations WHERE advisory_scope_id='vuln-intel://osv/npm'`).
		Scan(&class, &kinds); err != nil {
		t.Fatalf("read osv registration: %v", err)
	}
	if class != "osv" || !authorizedImpactAllowedKinds(class, kinds) {
		t.Fatalf("osv registration class = %q, kinds = %s, want osv with kinds the reader allows", class, kinds)
	}
}

// TestPublicAdvisoryReconcileThroughStoreRefusesGHSAConfigLive drives the Go
// store entrypoint. A configuration the workflow ghsa validator accepts passes
// the Go check and is refused by the function, so admission for configuration
// parity cannot become a registration. A key spelled in another case stops
// earlier: the store's validation refuses it before any SQL runs.
func TestPublicAdvisoryReconcileThroughStoreRefusesGHSAConfigLive(t *testing.T) {
	database, _ := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	principals := &scannerProofPrincipals{admin: database}
	principals.producer = mustCreateScannerProofRole(t, database, ctx, "producer")
	principals.coordinator = mustCreateScannerProofRole(t, database, ctx, "coordinator")
	principals.worker = mustCreateScannerProofRole(t, database, ctx, "worker")
	mustGrantScannerProofPrivileges(t, database, ctx, principals.producer, "typed-source-producer")
	mustGrantScannerProofPrivileges(t, database, ctx, principals.coordinator, "authority-control")
	mustGrantScannerProofPrivileges(t, database, ctx, principals.worker, "runtime-writer")
	mustSeedScannerProofPolicy(t, database, ctx, principals)

	// The ghsa target is a rolling anchor since #10516: the workflow
	// validator refuses it without derive_ghsa_rolling_windows.enabled
	// and requires refresh_interval on the anchor.
	ghsaTarget := publicAdvisoryProofGHSATarget("reviewed", "vuln-intel://ghsa/reviewed")
	ghsaTarget["refresh_interval"] = "1h"
	configuration, err := json.Marshal(map[string]any{
		"targets":                     []any{ghsaTarget},
		"derive_ghsa_rolling_windows": map[string]any{"enabled": true},
		"public_advisory_targets":     []any{map[string]any{"scope_id": "vuln-intel://ghsa/reviewed", "max_age_seconds": 3600}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := workflow.ValidateVulnerabilityIntelligenceCollectorConfiguration(string(configuration)); err != nil {
		t.Fatalf("workflow validator rejects the fixture: %v", err)
	}
	store := NewWorkflowControlStore(SQLDB{DB: principals.coordinator.db})
	reconcileConfiguration := func(document string) error {
		return store.ReconcilePublicAdvisoryRegistrations(ctx, workflow.PublicAdvisoryPolicyReconcile{
			ExpectedRevision: 0, DesiredRevision: 1,
			ExpectedRoleRevision: scannerProofPolicyRevision, ExpectedDatabaseIdentity: scannerProofDatabaseIdentity,
			Instances: []workflow.DesiredCollectorInstance{{
				InstanceID: "ghsa-store", CollectorKind: scope.CollectorVulnerabilityIntelligence,
				Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: true,
				DisplayName: "ghsa-store", Configuration: document,
			}},
		})
	}

	respelled := strings.Replace(string(configuration), `"advisory_type"`, `"ADVISORY_TYPE"`, 1)
	if respelled == string(configuration) {
		t.Fatal("fixture has no advisory_type key to respell")
	}
	if err := reconcileConfiguration(respelled); err == nil || !strings.Contains(err.Error(), "ADVISORY_TYPE") {
		t.Fatalf("store error for the respelled key = %v, want the validator's refusal naming it", err)
	}

	if err := reconcileConfiguration(string(configuration)); err == nil || !strings.Contains(err.Error(), publicAdvisoryUnsupportedText) {
		t.Fatalf("ReconcilePublicAdvisoryRegistrations() error = %v, want %q", err, publicAdvisoryUnsupportedText)
	}
	var count int
	if err := database.QueryRowContext(ctx,
		`SELECT count(*) FROM scanner_advisory_public_registrations`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("registrations after the refusal = %d, err = %v, want 0", count, err)
	}
}
