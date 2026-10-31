// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9299 disposable PostgreSQL source-binding authorization and revocation proof
//eshu:readiness:env ESHU_SCANNER_READINESS_AUTH_PROOF_DISPOSABLE ESHU_SCANNER_READINESS_AUTH_PROOF_DSN

import (
	"context"
	"database/sql"
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

func seedReadinessSourceBindingFixture(t *testing.T, ctx context.Context, database *sql.DB) string {
	t.Helper()
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
	seedReadinessCompletedReceipts(t, ctx, database)
	return digest
}

// seedReadinessCompletedReceipts completes the receipt chain behind the
// fixture bindings (#9468): a published source generation, live activation,
// producer grant, authority epochs, role policy, completed attempts with the
// exact authority vector, receipts, and fact associations for the analysis,
// package, and warning facts of each target. Epoch literals are distinct per
// pin so a cross-wired join fails instead of passing.
func seedReadinessCompletedReceipts(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO eshu_security.role_policy_state
  (policy_revision, database_oid, database_identity, system_identifier,
   inventory_version, owner_role_oid, authority_epoch, active)
SELECT 3, (SELECT oid FROM pg_database WHERE datname=current_database()),
  '33333333-3333-3333-3333-333333333333',
  (SELECT system_identifier FROM pg_control_system()), 1,
  (SELECT oid FROM pg_roles WHERE rolname=current_user), 1, TRUE;
INSERT INTO scanner_running_image_producer_grants
  (principal_oid, producer_instance_id, source_scope_id, producer_class,
   observation_class, contract_version, grant_epoch, active)
SELECT 10, 'source-read', 'read-source-' || suffix, 'kubernetes_pod',
       'running_container', 1, 11, TRUE
FROM (VALUES ('a1'), ('a2'), ('b')) AS sources(suffix);
INSERT INTO scanner_running_image_generations
  (producer_instance_id, source_scope_id, source_generation_id, principal_oid,
   collection_fence, producer_grant_epoch, producer_class, observation_class,
   status, observed_from, observed_through, consistency_class, publication_epoch)
SELECT 'source-read', 'read-source-' || suffix, 'read-gen-' || suffix, 10,
       1, 11, 'kubernetes_pod', 'running_container',
       'published', now(), now(), 'observed_interval', 12
FROM (VALUES ('a1'), ('a2'), ('b')) AS sources(suffix);
INSERT INTO scanner_running_image_delivery_receipts
  (producer_instance_id, source_scope_id, source_generation_id, delivery_id,
   request_sha256)
SELECT 'source-read', 'read-source-' || suffix, 'read-gen-' || suffix,
       'read-delivery-' || suffix,
       'eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee'
FROM (VALUES ('a1'), ('a2'), ('b')) AS sources(suffix);
INSERT INTO scanner_running_image_observations
  (observation_id, producer_instance_id, source_scope_id, source_generation_id,
   delivery_id, object_id, slot_id, producer_class, observation_class,
   registry, repository, runtime_image_reference, runtime_digest, runtime_digest_kind,
   payload_sha256)
SELECT 9000 + row_number() OVER (),
       'source-read', 'read-source-' || suffix, 'read-gen-' || suffix,
       'read-delivery-' || suffix, 'read-object-' || suffix, 'regular:' || suffix,
       'kubernetes_pod', 'running_container',
       'registry.example.invalid', 'team/service',
       'registry.example.invalid/team/service@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
       'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'manifest',
       'ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff'
FROM (VALUES ('a1'), ('a2'), ('b')) AS sources(suffix);
INSERT INTO scanner_running_image_active_scopes
  (producer_instance_id, source_scope_id, published_generation_id,
   activation_epoch, active, published_at)
SELECT 'source-read', 'read-source-' || suffix, 'read-gen-' || suffix,
       12, TRUE, now()
FROM (VALUES ('a1'), ('a2'), ('b')) AS sources(suffix);
UPDATE scanner_authority_epochs SET authority_epoch=13, active=TRUE
WHERE authority_kind='collector_instance'
  AND authority_key=(jsonb_build_array('source-read'))::text;
UPDATE scanner_authority_epochs SET authority_epoch=17, active=TRUE
WHERE authority_kind='collector_instance'
  AND authority_key=(jsonb_build_array('scanner-read'))::text;
UPDATE scanner_authority_epochs SET authority_epoch=14, active=TRUE
WHERE authority_kind='tenant' AND authority_key LIKE '%read-tenant-%';
UPDATE scanner_authority_epochs SET authority_epoch=15, active=TRUE
WHERE authority_kind='workspace';
UPDATE scanner_authority_epochs SET authority_epoch=16, active=TRUE
WHERE authority_kind='scope_grant';
UPDATE scanner_image_target_sources AS binding SET
  canonical_observation_id=observation.observation_id,
  pinned_source_activation_epoch=12, pinned_producer_grant_epoch=11,
  pinned_source_instance_epoch=13, pinned_tenant_epoch=14,
  pinned_workspace_epoch=15, pinned_scope_grant_epoch=16,
  pinned_registry_epoch=17, pinned_role_policy_revision=3,
  pinned_database_identity='33333333-3333-3333-3333-333333333333'
FROM scanner_running_image_observations AS observation
WHERE observation.producer_instance_id='source-read'
  AND observation.source_scope_id=binding.source_scope_id
  AND observation.source_generation_id=binding.source_generation_id;
`); err != nil {
		t.Fatalf("seed scanner publication chain: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO scanner_image_target_attempts
  (work_item_id, scan_scope_id, binding_id, status, created_at, finished_at,
   claim_id, claim_fencing_token, claim_owner_id, completed_authority_vector)
SELECT 'read-work-' || suffix,
       CASE WHEN suffix='b' THEN 'read-target-b' ELSE 'read-target-a' END,
       'read-binding-' || suffix, 'completed', now(), now(),
       'read-claim-' || suffix, 7, 'read-owner',
       jsonb_build_object(
         'source_activation_epoch',12, 'producer_grant_epoch',11,
         'source_instance_epoch',13, 'tenant_epoch',14,
         'workspace_epoch',15, 'scope_grant_epoch',16,
         'registry_epoch',17, 'role_policy_revision',3,
         'database_identity','33333333-3333-3333-3333-333333333333')
FROM (VALUES ('a1'), ('a2'), ('b')) AS sources(suffix);
INSERT INTO scanner_image_scan_results
  (result_receipt_id, work_item_id, binding_id, scan_scope_id, scan_generation_id,
   claim_id, claim_fencing_token, claim_owner_id, completed_authority_vector, completed_at)
SELECT 'read-receipt-' || suffix, 'read-work-' || suffix, 'read-binding-' || suffix,
       CASE WHEN suffix='b' THEN 'read-target-b' ELSE 'read-target-a' END,
       CASE WHEN suffix='b' THEN 'read-scan-b' ELSE 'read-scan-a' END,
       'read-claim-' || suffix, 7, 'read-owner',
       jsonb_build_object(
         'source_activation_epoch',12, 'producer_grant_epoch',11,
         'source_instance_epoch',13, 'tenant_epoch',14,
         'workspace_epoch',15, 'scope_grant_epoch',16,
         'registry_epoch',17, 'role_policy_revision',3,
         'database_identity','33333333-3333-3333-3333-333333333333'),
       now()
FROM (VALUES ('a1'), ('a2'), ('b')) AS sources(suffix);
UPDATE fact_records SET fencing_token=7
WHERE fact_id IN ('analysis-target-a', 'os-a', 'warning-a', 'analysis-target-b', 'os-b', 'warning-b');
INSERT INTO scanner_image_scan_fact_associations
  (fact_id, result_receipt_id, scan_scope_id, scan_generation_id, fact_kind,
   payload_sha256, fencing_token)
SELECT fact.fact_id, 'read-receipt-' || receipt.suffix,
       fact.scope_id, fact.generation_id, fact.fact_kind,
       encode(sha256(convert_to(fact.payload::text, 'UTF8')), 'hex'),
       fact.fencing_token
FROM fact_records AS fact
JOIN (VALUES ('a1', ARRAY['warning-a']),
             ('a2', ARRAY['analysis-target-a', 'os-a']),
             ('b', ARRAY['analysis-target-b', 'os-b', 'warning-b'])) AS receipt(suffix, facts)
  ON fact.fact_id = ANY(receipt.facts);
`); err != nil {
		t.Fatalf("seed completed scan receipts: %v", err)
	}
}

func readReadinessAnalysisPackages(t *testing.T, ctx context.Context, database *sql.DB, digest, tenant string, scopeIDs ...string) (int, int) {
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

func readReadinessImageWarnings(t *testing.T, ctx context.Context, database *sql.DB, digest, tenant string, scopeIDs ...string) int {
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

func TestReadinessScannerSourceBindingAuthorizationLive(t *testing.T) {
	ctx, database := postgresproof.OpenDisposableDatabase(t,
		os.Getenv("ESHU_SCANNER_READINESS_AUTH_PROOF_DSN"),
		os.Getenv("ESHU_SCANNER_READINESS_AUTH_PROOF_DISPOSABLE"), 3*time.Minute)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap: %v", err)
	}
	digest := seedReadinessSourceBindingFixture(t, ctx, database)
	if analysis, packages := readReadinessAnalysisPackages(t, ctx, database, digest, "read-tenant-a", "image://derived/orphan"); analysis != 0 || packages != 0 {
		t.Errorf("unassociated derived scope leaked through static branch: analysis=%d packages=%d, want 0/0", analysis, packages)
	}
	if analysis, packages := readReadinessAnalysisPackages(t, ctx, database, digest, "read-tenant-a", "read-source-a1", "read-source-a2"); analysis != 1 || packages != 1 {
		t.Fatalf("tenant A duplicate bindings: analysis=%d packages=%d, want 1/1", analysis, packages)
	}
	if analysis, packages := readReadinessAnalysisPackages(t, ctx, database, digest, "read-tenant-b", "read-source-b"); analysis != 1 || packages != 1 {
		t.Fatalf("tenant B: analysis=%d packages=%d, want 1/1", analysis, packages)
	}
	if warnings := readReadinessImageWarnings(t, ctx, database, digest, "read-tenant-a", "read-source-a1", "read-source-a2"); warnings != 1 {
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
	if analysis, packages := readReadinessAnalysisPackages(t, ctx, database, digest, "read-tenant-a", "read-source-a1", "read-source-a2"); analysis != 0 || packages != 0 {
		t.Fatalf("disabled scanner domain: analysis=%d packages=%d, want 0/0", analysis, packages)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration =
        '{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"read-domain","registry":"registry.example.invalid"}]}'::jsonb
        WHERE instance_id='scanner-read';
-- Re-enable rewrites the scanner configuration, which bumps the collector
-- epoch; re-pin the bindings the way a re-admission would, rebuilding the
-- receipt and attempt vectors that embed the pins (#9468).
UPDATE scanner_image_target_sources SET pinned_registry_epoch=(
  SELECT authority_epoch FROM scanner_authority_epochs
  WHERE authority_kind='collector_instance'
    AND authority_key=(jsonb_build_array('scanner-read'))::text);
UPDATE scanner_image_scan_results AS receipt SET completed_authority_vector=(
  SELECT jsonb_build_object(
    'source_activation_epoch',binding.pinned_source_activation_epoch,
    'producer_grant_epoch',binding.pinned_producer_grant_epoch,
    'source_instance_epoch',binding.pinned_source_instance_epoch,
    'tenant_epoch',binding.pinned_tenant_epoch,
    'workspace_epoch',binding.pinned_workspace_epoch,
    'scope_grant_epoch',binding.pinned_scope_grant_epoch,
    'registry_epoch',binding.pinned_registry_epoch,
    'role_policy_revision',binding.pinned_role_policy_revision,
    'database_identity',binding.pinned_database_identity)
  FROM scanner_image_target_sources AS binding
  WHERE binding.binding_id=receipt.binding_id);
UPDATE scanner_image_target_attempts AS attempt SET completed_authority_vector=(
  SELECT receipt.completed_authority_vector
  FROM scanner_image_scan_results AS receipt
  WHERE receipt.work_item_id=attempt.work_item_id);`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE fact_records SET payload='{"collector_instance_id":"changed"}'::jsonb
        WHERE fact_id='source-fact-a1'`); err != nil {
		t.Fatal(err)
	}
	if analysis, packages := readReadinessAnalysisPackages(t, ctx, database, digest, "read-tenant-a", "read-source-a1"); analysis != 0 || packages != 0 {
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
WHERE scope_id='read-source-a1';
-- The source moved on: the scanner publication for read-gen-a1 is retired
-- with it, so the a1 binding goes stale while a2 stays live (#9468). The
-- later ingestion-only revert cannot resurrect the retired publication.
UPDATE scanner_running_image_generations SET status='failed'
WHERE producer_instance_id='source-read' AND source_scope_id='read-source-a1'
  AND source_generation_id='read-gen-a1';`); err != nil {
		t.Fatal(err)
	}
	if analysis, packages := readReadinessAnalysisPackages(t, ctx, database, digest, "read-tenant-a", "read-source-a1", "read-source-a2"); analysis != 1 || packages != 1 {
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
	if analysis, packages := readReadinessAnalysisPackages(t, ctx, database, digest, "read-tenant-a", "read-source-a1"); analysis != 0 || packages != 0 {
		t.Errorf("source generation ABA reused stale binding: analysis=%d packages=%d, want 0/0", analysis, packages)
	}
	if _, err := database.ExecContext(ctx, `UPDATE tenant_scope_grants SET tombstoned_at=now()
WHERE tenant_id='read-tenant-a' AND scope_id='read-source-a1'`); err != nil {
		t.Fatal(err)
	}
	if analysis, packages := readReadinessAnalysisPackages(t, ctx, database, digest, "read-tenant-a", "read-source-a1", "read-source-a2"); analysis != 1 || packages != 1 {
		t.Fatalf("tenant A one live binding remains after first grant revocation: analysis=%d packages=%d, want 1/1", analysis, packages)
	}
	if _, err := database.ExecContext(ctx, `UPDATE tenant_scope_grants SET tombstoned_at=now()
WHERE tenant_id='read-tenant-a' AND scope_id='read-source-a2'`); err != nil {
		t.Fatal(err)
	}
	if analysis, packages := readReadinessAnalysisPackages(t, ctx, database, digest, "read-tenant-a", "read-source-a1", "read-source-a2"); analysis != 0 || packages != 0 {
		t.Fatalf("tenant A revoked: analysis=%d packages=%d, want 0/0", analysis, packages)
	}
	if warnings := readReadinessImageWarnings(t, ctx, database, digest, "read-tenant-a", "read-source-a1", "read-source-a2"); warnings != 0 {
		t.Fatalf("tenant A revoked warnings=%d, want 0", warnings)
	}
	if analysis, packages := readReadinessAnalysisPackages(t, ctx, database, digest, "read-tenant-b", "read-source-b"); analysis != 1 || packages != 1 {
		t.Fatalf("tenant B after A revocation: analysis=%d packages=%d, want 1/1", analysis, packages)
	}
	if warnings := readReadinessImageWarnings(t, ctx, database, digest, "read-tenant-b", "read-source-b"); warnings != 1 {
		t.Fatalf("tenant B warnings after A revocation=%d, want 1", warnings)
	}
	if _, err := database.ExecContext(ctx, `UPDATE tenant_scope_grants SET tombstoned_at=NULL
WHERE tenant_id='read-tenant-a' AND scope_id IN ('read-source-a1','read-source-a2')`); err != nil {
		t.Fatal(err)
	}
	if analysis, packages := readReadinessAnalysisPackages(t, ctx, database, digest, "read-tenant-a", "read-source-a1", "read-source-a2"); analysis != 0 || packages != 0 {
		t.Errorf("same-policy regrant reused stale binding: analysis=%d packages=%d, want 0/0", analysis, packages)
	}
	if analysis, packages := readReadinessAnalysisPackages(t, ctx, database, digest, "read-tenant-a", "read-static"); analysis != 1 || packages != 0 {
		t.Fatalf("direct static scope: analysis=%d packages=%d, want 1/0", analysis, packages)
	}
}
