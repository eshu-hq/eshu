// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9299 disposable PostgreSQL source-binding authorization and revocation proof
//eshu:readiness:env ESHU_SCANNER_READINESS_AUTH_PROOF_DISPOSABLE ESHU_SCANNER_READINESS_AUTH_PROOF_DSN

import (
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestReadinessScannerSourceBindingAuthorizationLive proves that derived scan
// facts are visible only through a current source binding and grant. A second
// binding never multiplies one scan; revocation leaves the other tenant and a
// direct static scan unchanged.
func TestReadinessScannerSourceBindingAuthorizationLive(t *testing.T) {
	ctx, database := postgresproof.OpenDisposableDatabase(t,
		os.Getenv("ESHU_SCANNER_READINESS_AUTH_PROOF_DSN"),
		os.Getenv("ESHU_SCANNER_READINESS_AUTH_PROOF_DISPOSABLE"), 3*time.Minute)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap: %v", err)
	}
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := database.ExecContext(ctx, `
INSERT INTO collector_instances
  (instance_id, collector_kind, mode, enabled, claims_enabled, configuration,
   last_observed_at, created_at, updated_at)
VALUES
  ('scanner-read', 'scanner_worker', 'continuous', TRUE, TRUE,
   '{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"read-domain","registry":"registry.example.invalid"}]}'::jsonb,
   now(), now(), now()),
  ('source-read', 'kubernetes_live', 'continuous', TRUE, TRUE, '{}'::jsonb,
   now(), now(), now());
INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
   observed_at, ingested_at, status, active_generation_id)
VALUES
  ('read-source-a1', 'kubernetes', 'kubernetes_live', 'a1', 'kubernetes_live', 'a1', now(), now(), 'active', 'read-gen-a1'),
  ('read-source-a2', 'kubernetes', 'kubernetes_live', 'a2', 'kubernetes_live', 'a2', now(), now(), 'active', 'read-gen-a2'),
  ('read-source-b', 'kubernetes', 'kubernetes_live', 'b', 'kubernetes_live', 'b', now(), now(), 'active', 'read-gen-b'),
  ('read-target-a', 'container_image', 'scanner_worker', 'a', 'scanner_worker', 'a', now(), now(), 'active', 'read-scan-a'),
  ('read-target-b', 'container_image', 'scanner_worker', 'b', 'scanner_worker', 'b', now(), now(), 'active', 'read-scan-b'),
  ('read-static', 'container_image', 'scanner_worker', 'static', 'scanner_worker', 'static', now(), now(), 'active', 'read-scan-static'),
  ('image://derived/orphan', 'container_image', 'scanner_worker', 'orphan', 'scanner_worker', 'orphan', now(), now(), 'active', 'read-scan-orphan');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES
  ('read-gen-a1', 'read-source-a1', 'snapshot', now(), now(), 'active'),
  ('read-gen-a2', 'read-source-a2', 'snapshot', now(), now(), 'active'),
  ('read-gen-b', 'read-source-b', 'snapshot', now(), now(), 'active'),
  ('read-scan-a', 'read-target-a', 'snapshot', now(), now(), 'active'),
  ('read-scan-b', 'read-target-b', 'snapshot', now(), now(), 'active'),
  ('read-scan-static', 'read-static', 'snapshot', now(), now(), 'active'),
  ('read-scan-orphan', 'image://derived/orphan', 'snapshot', now(), now(), 'active');
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
   source_fact_key, observed_at, ingested_at, payload)
SELECT 'source-fact-' || suffix, 'read-source-' || suffix, 'read-gen-' || suffix,
       'kubernetes_live.pod_template', 'source-' || suffix, 'kubernetes_live',
       'source-' || suffix, now(), now(), '{"collector_instance_id":"source-read"}'::jsonb
FROM (VALUES ('a1'), ('a2'), ('b')) AS sources(suffix);
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
   source_fact_key, observed_at, ingested_at, payload)
SELECT 'analysis-' || suffix, 'read-' || suffix, 'read-scan-' || replace(suffix, 'target-', ''),
       'scanner_worker.analysis', 'analysis-' || suffix, 'scanner_worker',
       'analysis-' || suffix, now(), now(),
       jsonb_build_object('image_digest', 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
                          'image_reference', 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
FROM (VALUES ('target-a'), ('target-b'), ('static')) AS scans(suffix);
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
   source_fact_key, observed_at, ingested_at, payload)
VALUES ('analysis-orphan', 'image://derived/orphan', 'read-scan-orphan',
        'scanner_worker.analysis', 'analysis-orphan', 'scanner_worker',
        'analysis-orphan', now(), now(),
        '{"image_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}'::jsonb);
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
   source_fact_key, observed_at, ingested_at, payload)
SELECT 'os-' || suffix, 'read-target-' || suffix, 'read-scan-' || suffix,
       'vulnerability.os_package', 'os-' || suffix, 'scanner_worker',
       'os-' || suffix, now(), now(), '{}'::jsonb
FROM (VALUES ('a'), ('b')) AS scans(suffix);
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
   source_fact_key, observed_at, ingested_at, payload)
SELECT 'warning-' || suffix, 'read-target-' || suffix, 'read-scan-' || suffix,
       'scanner_worker.warning', 'warning-' || suffix, 'scanner_worker',
       'warning-' || suffix, now(), now(),
       jsonb_build_object('target_kind', 'image', 'reason', 'analyzer_not_configured',
                          'image_digest', 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
FROM (VALUES ('a'), ('b')) AS scans(suffix);
`); err != nil {
		t.Fatalf("seed active source and scan facts: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO tenants (tenant_id, status, policy_revision_hash, created_at, updated_at)
VALUES ('read-tenant-a', 'active', 'read-policy', now(), now()),
       ('read-tenant-b', 'active', 'read-policy', now(), now());
INSERT INTO workspaces (tenant_id, workspace_id, status, policy_revision_hash, created_at, updated_at)
VALUES ('read-tenant-a', 'read-workspace', 'active', 'read-policy', now(), now()),
       ('read-tenant-b', 'read-workspace', 'active', 'read-policy', now(), now());
INSERT INTO tenant_scope_grants
  (tenant_id, workspace_id, scope_id, subject_class, grant_source,
   policy_revision_hash, effective_at, created_at, updated_at)
SELECT CASE WHEN suffix='b' THEN 'read-tenant-b' ELSE 'read-tenant-a' END,
       'read-workspace', 'read-source-' || suffix, 'collector', 'operator',
       'read-policy', now() - interval '1 minute', now(), now()
FROM (VALUES ('a1'), ('a2'), ('b')) AS sources(suffix);
INSERT INTO scanner_image_targets
  (scan_scope_id, authorization_namespace, tenant_id, workspace_id,
   subject_class, authorization_domain, manifest_digest)
VALUES
  ('read-target-a', 'hosted', 'read-tenant-a', 'read-workspace', 'collector', 'read-domain', 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'),
  ('read-target-b', 'hosted', 'read-tenant-b', 'read-workspace', 'collector', 'read-domain', 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa');
INSERT INTO scanner_image_target_sources
  (binding_id, scan_scope_id, source_fact_id, source_collector_instance_id,
   source_scope_id, source_generation_id, scanner_collector_instance_id,
   policy_revision_hash, registry, repository, runtime_digest, runtime_digest_kind,
   runtime_image_reference, registry_domain_config_sha256, source_payload_sha256, validated_at)
SELECT 'read-binding-' || suffix,
       CASE WHEN suffix='b' THEN 'read-target-b' ELSE 'read-target-a' END,
       fact.fact_id, 'source-read', fact.scope_id, fact.generation_id,
       'scanner-read', 'read-policy', 'registry.example.invalid', 'team/service',
       'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
       'manifest', 'registry.example.invalid/team/service@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
       encode(sha256(convert_to(domain.value::text, 'UTF8')), 'hex'),
       encode(sha256(convert_to(fact.payload::text, 'UTF8')), 'hex'), now()
FROM (VALUES ('a1'), ('a2'), ('b')) AS sources(suffix)
JOIN fact_records AS fact ON fact.fact_id='source-fact-' || suffix
CROSS JOIN collector_instances AS scanner
CROSS JOIN LATERAL jsonb_array_elements(scanner.configuration->'registry_domains') AS domain(value)
WHERE scanner.instance_id='scanner-read';
`); err != nil {
		t.Fatalf("seed target bindings and grants: %v", err)
	}
	read := func(tenant string, scopeIDs ...string) (int, int) {
		t.Helper()
		readCtx := auth.ContextWithAuthContext(ctx, auth.AuthContext{
			Mode: auth.AuthModeScoped, TenantID: tenant, WorkspaceID: "read-workspace",
			SubjectClass: "collector", PolicyRevisionHash: "read-policy", AllowedScopeIDs: scopeIDs,
		})
		snapshot, err := NewPostgresReadinessStore(database).readSupplyChainImpactReadiness(
			readCtx, readinessSQLQueryer{database: database},
			ReadinessQuery{SubjectDigest: digest}, readinessTarget{})
		if err != nil {
			t.Fatalf("readiness store: %v", err)
		}
		counts := map[string]int{}
		for _, family := range snapshot.EvidenceSources {
			counts[family.Family] = family.FactCount
		}
		return counts["scanner_worker.analysis"], counts["vulnerability.os_package"]
	}
	readWarnings := func(tenant string, scopeIDs ...string) int {
		t.Helper()
		readCtx := auth.ContextWithAuthContext(ctx, auth.AuthContext{
			Mode: auth.AuthModeScoped, TenantID: tenant, WorkspaceID: "read-workspace",
			SubjectClass: "collector", PolicyRevisionHash: "read-policy", AllowedScopeIDs: scopeIDs,
		})
		snapshot, err := NewPostgresReadinessStore(database).readSupplyChainImpactReadiness(
			readCtx, readinessSQLQueryer{database: database},
			ReadinessQuery{SubjectDigest: digest}, readinessTarget{})
		if err != nil {
			t.Fatalf("warning readiness store: %v", err)
		}
		count := 0
		for _, target := range snapshot.UnsupportedTargets {
			if target.TargetKind == UnsupportedTargetKindImageTarget {
				count += target.Count
			}
		}
		return count
	}
	if analysis, packages := read("read-tenant-a", "read-source-a1", "read-source-a2"); analysis != 1 || packages != 1 {
		t.Fatalf("tenant A duplicate bindings: analysis=%d packages=%d, want 1/1", analysis, packages)
	}
	if analysis, packages := read("read-tenant-b", "read-source-b"); analysis != 1 || packages != 1 {
		t.Fatalf("tenant B: analysis=%d packages=%d, want 1/1", analysis, packages)
	}
	if analysis, packages := read("read-tenant-a", "image://derived/orphan"); analysis != 0 || packages != 0 {
		t.Errorf("unassociated derived scope leaked through static branch: analysis=%d packages=%d, want 0/0", analysis, packages)
	}
	if warnings := readWarnings("read-tenant-a", "read-source-a1", "read-source-a2"); warnings != 1 {
		t.Fatalf("tenant A warnings=%d, want 1", warnings)
	}
	// A hosted AllScopes flag without source grants must not bypass binding
	// authorization even when a direct store caller skips route middleware.
	allScopesContext := auth.ContextWithAuthContext(ctx, auth.AuthContext{
		Mode: auth.AuthModeScoped, TenantID: "read-tenant-a", WorkspaceID: "read-workspace",
		SubjectClass: "collector", PolicyRevisionHash: "read-policy", AllScopes: true,
	})
	allScopesSnapshot, err := NewPostgresReadinessStore(database).readSupplyChainImpactReadiness(
		allScopesContext, readinessSQLQueryer{database: database},
		ReadinessQuery{SubjectDigest: digest}, readinessTarget{})
	if err != nil {
		t.Fatalf("AllScopes readiness store: %v", err)
	}
	for _, family := range allScopesSnapshot.EvidenceSources {
		if (family.Family == EvidenceFamilyScannerWorkerAnalysis ||
			family.Family == EvidenceFamilyVulnerabilityOSPackage) && family.FactCount != 0 {
			t.Fatalf("AllScopes-only %s count=%d, want 0", family.Family, family.FactCount)
		}
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration =
        '{"derive_from_running_images":false,"registry_domains":[{"authorization_domain":"read-domain","registry":"registry.example.invalid"}]}'::jsonb
        WHERE instance_id='scanner-read'`); err != nil {
		t.Fatal(err)
	}
	if analysis, packages := read("read-tenant-a", "read-source-a1", "read-source-a2"); analysis != 0 || packages != 0 {
		t.Fatalf("disabled scanner domain: analysis=%d packages=%d, want 0/0", analysis, packages)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration =
        '{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"read-domain","registry":"registry.example.invalid"}]}'::jsonb
        WHERE instance_id='scanner-read'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE fact_records SET payload='{"collector_instance_id":"changed"}'::jsonb
        WHERE fact_id='source-fact-a1'`); err != nil {
		t.Fatal(err)
	}
	if analysis, packages := read("read-tenant-a", "read-source-a1"); analysis != 0 || packages != 0 {
		t.Fatalf("changed exact source: analysis=%d packages=%d, want 0/0", analysis, packages)
	}
	if _, err := database.ExecContext(ctx, `UPDATE fact_records SET payload='{"collector_instance_id":"source-read"}'::jsonb
        WHERE fact_id='source-fact-a1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
UPDATE scope_generations SET status='superseded', superseded_at=now()
WHERE generation_id='read-gen-a1';
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('read-gen-a1-replacement', 'read-source-a1', 'snapshot', now(), now(), 'active');
UPDATE ingestion_scopes SET active_generation_id='read-gen-a1-replacement'
WHERE scope_id='read-source-a1';`); err != nil {
		t.Fatal(err)
	}
	if analysis, packages := read("read-tenant-a", "read-source-a1", "read-source-a2"); analysis != 1 || packages != 1 {
		t.Fatalf("tenant A one live binding remains after source generation switch: analysis=%d packages=%d, want 1/1", analysis, packages)
	}
	if _, err := database.ExecContext(ctx, `
UPDATE scope_generations SET status='superseded', superseded_at=now()
WHERE generation_id='read-gen-a1-replacement';
UPDATE scope_generations SET status='active', superseded_at=NULL
WHERE generation_id='read-gen-a1';
UPDATE ingestion_scopes SET active_generation_id='read-gen-a1'
WHERE scope_id='read-source-a1';`); err != nil {
		t.Fatal(err)
	}
	if analysis, packages := read("read-tenant-a", "read-source-a1"); analysis != 0 || packages != 0 {
		t.Errorf("source generation ABA reused stale binding: analysis=%d packages=%d, want 0/0", analysis, packages)
	}
	if _, err := database.ExecContext(ctx, `UPDATE tenant_scope_grants SET tombstoned_at=now()
WHERE tenant_id='read-tenant-a' AND scope_id='read-source-a1'`); err != nil {
		t.Fatal(err)
	}
	if analysis, packages := read("read-tenant-a", "read-source-a1", "read-source-a2"); analysis != 1 || packages != 1 {
		t.Fatalf("tenant A one live binding remains after first grant revocation: analysis=%d packages=%d, want 1/1", analysis, packages)
	}
	if _, err := database.ExecContext(ctx, `UPDATE tenant_scope_grants SET tombstoned_at=now()
WHERE tenant_id='read-tenant-a' AND scope_id='read-source-a2'`); err != nil {
		t.Fatal(err)
	}
	if analysis, packages := read("read-tenant-a", "read-source-a1", "read-source-a2"); analysis != 0 || packages != 0 {
		t.Fatalf("tenant A revoked: analysis=%d packages=%d, want 0/0", analysis, packages)
	}
	if warnings := readWarnings("read-tenant-a", "read-source-a1", "read-source-a2"); warnings != 0 {
		t.Fatalf("tenant A revoked warnings=%d, want 0", warnings)
	}
	if warnings := readWarnings("read-tenant-b", "read-source-b"); warnings != 1 {
		t.Fatalf("tenant B warnings after A revocation=%d, want 1", warnings)
	}
	global, err := NewPostgresReadinessStore(database).readSupplyChainImpactReadiness(
		ctx, readinessSQLQueryer{database: database},
		ReadinessQuery{SubjectDigest: digest}, readinessTarget{})
	if err != nil {
		t.Fatalf("trusted global readiness store: %v", err)
	}
	if analysis, packages := evidenceFactCount(global.EvidenceSources, EvidenceFamilyScannerWorkerAnalysis),
		evidenceFactCount(global.EvidenceSources, EvidenceFamilyVulnerabilityOSPackage); analysis != 2 || packages != 1 {
		t.Errorf("trusted global after A revocation: analysis=%d packages=%d, want B plus static 2/1", analysis, packages)
	}
	sharedCtx := auth.ContextWithAuthContext(ctx, auth.AuthContext{Mode: auth.AuthModeShared})
	shared, err := NewPostgresReadinessStore(database).readSupplyChainImpactReadiness(
		sharedCtx, readinessSQLQueryer{database: database},
		ReadinessQuery{SubjectDigest: digest}, readinessTarget{})
	if err != nil {
		t.Fatalf("shared-mode readiness store: %v", err)
	}
	if analysis, packages := evidenceFactCount(shared.EvidenceSources, EvidenceFamilyScannerWorkerAnalysis),
		evidenceFactCount(shared.EvidenceSources, EvidenceFamilyVulnerabilityOSPackage); analysis != 2 || packages != 1 {
		t.Errorf("shared-mode after A revocation: analysis=%d packages=%d, want B plus static 2/1", analysis, packages)
	}
	if _, err := database.ExecContext(ctx, `UPDATE tenant_scope_grants SET tombstoned_at=NULL
WHERE tenant_id='read-tenant-a' AND scope_id IN ('read-source-a1','read-source-a2')`); err != nil {
		t.Fatal(err)
	}
	if analysis, packages := read("read-tenant-a", "read-source-a1", "read-source-a2"); analysis != 0 || packages != 0 {
		t.Errorf("same-policy regrant reused stale binding: analysis=%d packages=%d, want 0/0", analysis, packages)
	}
	if analysis, packages := read("read-tenant-a", "read-static"); analysis != 1 || packages != 0 {
		t.Fatalf("direct static scope: analysis=%d packages=%d, want 1/0", analysis, packages)
	}
}
