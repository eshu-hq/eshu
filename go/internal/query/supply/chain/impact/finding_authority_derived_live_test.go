// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9468 read proof for the derived finding-authority gate
//eshu:readiness:env ESHU_SCANNER_FINDING_AUTH_PROOF_DISPOSABLE ESHU_SCANNER_FINDING_AUTH_PROOF_DSN

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

// TestDerivedFindingAuthorityOpensBehindCurrentPublicationLive proves the
// finding-authority derived branch (#9468): a derived/active finding with a
// publication association surfaces iff its publication is current, the
// attempt and receipt vectors match, the source chain is live, and its
// public advisory dependency resolves. A classified finding with no
// association, a wrong-tenant caller, and a tombstoned grant all stay dark.
func TestDerivedFindingAuthorityOpensBehindCurrentPublicationLive(t *testing.T) {
	ctx, database := postgresproof.OpenDisposableDatabase(t,
		os.Getenv("ESHU_SCANNER_FINDING_AUTH_PROOF_DSN"),
		os.Getenv("ESHU_SCANNER_FINDING_AUTH_PROOF_DISPOSABLE"), 3*time.Minute)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap: %v", err)
	}
	seedFindingAuthorityDerivedFixture(t, ctx, database)
	checkFindingAuthorityDerivedReads(t, ctx, database)
}

func seedFindingAuthorityDerivedFixture(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO collector_instances
  (instance_id,collector_kind,mode,enabled,claims_enabled,configuration,last_observed_at,created_at,updated_at)
VALUES ('fa-scanner','scanner_worker','continuous',TRUE,TRUE,
        '{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"fa-domain","registry":"registry.example.invalid"}]}'::jsonb,
        now(),now(),now()),
       ('fa-source','kubernetes_live','continuous',TRUE,TRUE,'{}'::jsonb,now(),now(),now()),
       ('fa-advisory','vulnerability_intelligence','continuous',TRUE,TRUE,
        '{"provider_id":"fa-provider"}'::jsonb,now(),now(),now());
INSERT INTO ingestion_scopes
  (scope_id,scope_kind,source_system,source_key,collector_kind,partition_key,
   observed_at,ingested_at,status,active_generation_id)
VALUES ('fa-source','kubernetes','kubernetes_live','fa-source','kubernetes_live','fa-source',now(),now(),'active','fa-gen'),
       ('fa-target','container_image','scanner_worker','fa-target','scanner_worker','fa-target',now(),now(),'active','fa-scan'),
       ('fa-advisory','vulnerability','vulnerability_intelligence','fa-advisory','vulnerability_intelligence','fa-advisory',now(),now(),'active','fa-adv-gen'),
       ('fa-find','finding','reducer','fa-find','reducer','fa-find',now(),now(),'active','fa-find-gen');
INSERT INTO scope_generations (generation_id,scope_id,trigger_kind,observed_at,ingested_at,status)
VALUES ('fa-gen','fa-source','snapshot',now(),now(),'active'),
       ('fa-scan','fa-target','snapshot',now(),now(),'active'),
       ('fa-adv-gen','fa-advisory','snapshot',now(),now(),'active'),
       ('fa-find-gen','fa-find','snapshot',now(),now(),'active');
INSERT INTO tenants (tenant_id,status,policy_revision_hash,created_at,updated_at)
VALUES ('fa-tenant','active','fa-policy',now(),now());
INSERT INTO workspaces (tenant_id,workspace_id,status,policy_revision_hash,created_at,updated_at)
VALUES ('fa-tenant','fa-workspace','active','fa-policy',now(),now());
INSERT INTO tenant_scope_grants
  (tenant_id,workspace_id,scope_id,subject_class,grant_source,policy_revision_hash,
   effective_at,created_at,updated_at)
VALUES ('fa-tenant','fa-workspace','fa-source','collector','operator','fa-policy',now()-interval '1 minute',now(),now());
INSERT INTO scanner_image_targets
  (scan_scope_id,authorization_namespace,tenant_id,workspace_id,subject_class,
   authorization_domain,manifest_digest)
VALUES ('fa-target','hosted','fa-tenant','fa-workspace','collector','fa-domain',
        'sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff');
INSERT INTO scanner_image_target_sources
  (binding_id,scan_scope_id,source_scope_id,source_generation_id,source_collector_instance_id,
   scanner_collector_instance_id,policy_revision_hash,registry,repository,
   runtime_digest,runtime_digest_kind,runtime_image_reference,source_fact_id,source_payload_sha256,
   registry_domain_config_sha256,validated_at)
VALUES ('fa-binding','fa-target','fa-source','fa-gen','fa-source','fa-scanner','fa-policy',

        'registry.example.invalid','team/service',
        'sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff','manifest',
        'registry.example.invalid/team/service@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',
        'fa-source-fact',
        encode(sha256(convert_to('{"seen":true}'::text,'UTF8')),'hex'),
        encode(sha256(convert_to((SELECT (value::text) FROM jsonb_array_elements(
          (SELECT configuration->'registry_domains' FROM collector_instances WHERE instance_id='fa-scanner')) AS value
          WHERE value->>'authorization_domain'='fa-domain'),'UTF8')),'hex'),now());
`); err != nil {
		t.Fatalf("seed finding-authority base: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id,scope_id,generation_id,fact_kind,stable_fact_key,source_system,
   source_fact_key,collector_kind,observed_at,ingested_at,payload,fencing_token)
VALUES ('fa-source-fact','fa-source','fa-gen','kubernetes_live.pod','fa-source-fact','kubernetes_live',
        'fa-source-fact','kubernetes_live',now(),now(),'{"seen":true}'::jsonb,0),
       ('fa-analysis','fa-target','fa-scan','scanner_worker.analysis','fa-analysis','scanner_worker',
        'fa-analysis','scanner_worker',now(),now(),
        '{"analyzer":"fa","target_kind":"running_image","target_locator_hash":"fa","analysis_status":"completed","coverage_status":"scanned","image_reference":"registry.example.invalid/team/service:1.0","image_digest":"sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff","evidence_source":"scanner_worker","extraction_reason":"scheduled_scan"}'::jsonb,9),
       ('fa-finding','fa-find','fa-find-gen','reducer_supply_chain_impact_finding','fa-finding','reducer',
        'fa-finding','reducer',now(),now(),
        '{"finding_id":"fa-finding-1","cve_id":"CVE-2026-FA","package_id":"pkg:deb/debian/falib","subject_digest":"sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff","impact_status":"affected_exact","evidence_fact_ids":["fa-analysis"]}'::jsonb,0),
       ('fa-finding-noassoc','fa-find','fa-find-gen','reducer_supply_chain_impact_finding','fa-finding-noassoc','reducer',
        'fa-finding-noassoc','reducer',now(),now(),
        '{"finding_id":"fa-finding-2","cve_id":"CVE-2026-FA","package_id":"pkg:deb/debian/falib","subject_digest":"sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff","impact_status":"affected_exact","evidence_fact_ids":[]}'::jsonb,0),
       ('fa-cve','fa-advisory','fa-adv-gen','vulnerability.cve','fa-cve','vulnerability_intelligence',
        'fa-cve','vulnerability_intelligence',now(),now(),'{"cve_id":"CVE-2026-FA"}'::jsonb,8);
INSERT INTO eshu_security.role_policy_state
  (policy_revision,database_oid,database_identity,system_identifier,
   inventory_version,owner_role_oid,authority_epoch,active)
SELECT 5,(SELECT oid FROM pg_database WHERE datname=current_database()),
  '55555555-5555-5555-5555-555555555555',
  (SELECT system_identifier FROM pg_control_system()),1,
  (SELECT oid FROM pg_roles WHERE rolname=current_user),1,TRUE;
INSERT INTO scanner_running_image_producer_grants
  (principal_oid,producer_instance_id,source_scope_id,producer_class,
   observation_class,contract_version,grant_epoch,active)
VALUES (20,'fa-source','fa-source','kubernetes_pod','running_container',1,21,TRUE);
INSERT INTO scanner_running_image_generations
  (producer_instance_id,source_scope_id,source_generation_id,principal_oid,
   collection_fence,producer_grant_epoch,producer_class,observation_class,
   status,observed_from,observed_through,consistency_class,publication_epoch)
VALUES ('fa-source','fa-source','fa-gen',20,1,21,'kubernetes_pod','running_container',
        'published',now(),now(),'observed_interval',22);
INSERT INTO scanner_running_image_delivery_receipts
  (producer_instance_id,source_scope_id,source_generation_id,delivery_id,request_sha256)
VALUES ('fa-source','fa-source','fa-gen','fa-delivery',
        'eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee');
INSERT INTO scanner_running_image_observations
  (observation_id,producer_instance_id,source_scope_id,source_generation_id,
   delivery_id,object_id,slot_id,producer_class,observation_class,
   registry,repository,runtime_image_reference,runtime_digest,runtime_digest_kind,payload_sha256)
VALUES (9101,'fa-source','fa-source','fa-gen','fa-delivery','fa-object','regular:fa',
        'kubernetes_pod','running_container','registry.example.invalid','team/service',
        'registry.example.invalid/team/service@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',
        'sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff','manifest',
        'ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff');
INSERT INTO scanner_running_image_active_scopes
  (producer_instance_id,source_scope_id,published_generation_id,activation_epoch,active,published_at)
VALUES ('fa-source','fa-source','fa-gen',22,TRUE,now());
UPDATE scanner_authority_epochs SET authority_epoch=23,active=TRUE
WHERE authority_kind='collector_instance'
  AND authority_key=(jsonb_build_array('fa-source'))::text;
UPDATE scanner_authority_epochs SET authority_epoch=27,active=TRUE
WHERE authority_kind='collector_instance'
  AND authority_key=(jsonb_build_array('fa-scanner'))::text;
UPDATE scanner_authority_epochs SET authority_epoch=24,active=TRUE
WHERE authority_kind='tenant';
UPDATE scanner_authority_epochs SET authority_epoch=25,active=TRUE
WHERE authority_kind='workspace';
UPDATE scanner_authority_epochs SET authority_epoch=26,active=TRUE
WHERE authority_kind='scope_grant';
UPDATE scanner_image_target_sources AS binding SET
  canonical_observation_id=9101,
  pinned_source_activation_epoch=22,pinned_producer_grant_epoch=21,
  pinned_source_instance_epoch=23,pinned_tenant_epoch=24,
  pinned_workspace_epoch=25,pinned_scope_grant_epoch=26,
  pinned_registry_epoch=27,pinned_role_policy_revision=5,
  pinned_database_identity='55555555-5555-5555-5555-555555555555'
WHERE binding_id='fa-binding';
`); err != nil {
		t.Fatalf("seed finding-authority scan chain: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO scanner_image_target_attempts
  (work_item_id,scan_scope_id,binding_id,status,created_at,finished_at,
   claim_id,claim_fencing_token,claim_owner_id,completed_authority_vector)
VALUES ('fa-work','fa-target','fa-binding','completed',now(),now(),
        'fa-claim',7,'fa-owner',
        jsonb_build_object(
          'source_activation_epoch',22,'producer_grant_epoch',21,
          'source_instance_epoch',23,'tenant_epoch',24,
          'workspace_epoch',25,'scope_grant_epoch',26,
          'registry_epoch',27,'role_policy_revision',5,
          'database_identity','55555555-5555-5555-5555-555555555555'));
INSERT INTO scanner_image_scan_results
  (result_receipt_id,work_item_id,binding_id,scan_scope_id,scan_generation_id,
   claim_id,claim_fencing_token,claim_owner_id,completed_authority_vector,completed_at)
VALUES ('fa-receipt','fa-work','fa-binding','fa-target','fa-scan',
        'fa-claim',7,'fa-owner',
        jsonb_build_object(
          'source_activation_epoch',22,'producer_grant_epoch',21,
          'source_instance_epoch',23,'tenant_epoch',24,
          'workspace_epoch',25,'scope_grant_epoch',26,
          'registry_epoch',27,'role_policy_revision',5,
          'database_identity','55555555-5555-5555-5555-555555555555'),
        now());
INSERT INTO scanner_image_scan_fact_associations
  (fact_id,result_receipt_id,scan_scope_id,scan_generation_id,fact_kind,
   payload_sha256,fencing_token)
SELECT fact.fact_id,'fa-receipt','fa-target','fa-scan',fact.fact_kind,
       encode(sha256(convert_to(fact.payload::text,'UTF8')),'hex'),fact.fencing_token
FROM fact_records AS fact WHERE fact.fact_id='fa-analysis';
INSERT INTO scanner_image_result_publications
  (publication_id,scan_scope_id,scan_generation_id,binding_id,work_item_id,
   impact_scope_id,impact_generation_id,impact_fencing_token,
   scan_result_receipt_id,result_set_sha256,advisory_set_sha256,analysis_version,
   role_policy_revision,database_identity,status,published_at)
VALUES ('fa-pub','fa-target','fa-scan','fa-binding','fa-work',
        'fa-impact','fa-impact-gen',9,
        'fa-receipt','aa','bb','v1',
        5,'55555555-5555-5555-5555-555555555555','current',now());
INSERT INTO scanner_image_result_fact_classifications
  (finding_fact_id,source_class,lifecycle,scan_scope_id)
VALUES ('fa-finding','derived','active','fa-target'),
       ('fa-finding-noassoc','derived','active','fa-target');
INSERT INTO scanner_image_result_fact_associations
  (finding_fact_id,publication_id,scan_scope_id,scan_generation_id,tenant_id,workspace_id)
VALUES ('fa-finding','fa-pub','fa-target','fa-scan','fa-tenant','fa-workspace');
INSERT INTO scanner_image_result_evidence_associations
  (finding_fact_id,evidence_fact_id,publication_id,scan_scope_id,scan_generation_id,evidence_class)
VALUES ('fa-finding','fa-analysis','fa-pub','fa-target','fa-scan','scan_intrinsic');
`); err != nil {
		t.Fatalf("seed finding-authority publication: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO scanner_authority_epochs (authority_kind,authority_key,authority_epoch,active)
VALUES ('advisory_scope',(jsonb_build_array('fa-advisory'))::text,34,TRUE),
       ('collector_instance',(jsonb_build_array('fa-advisory'))::text,31,TRUE)
ON CONFLICT (authority_kind,authority_key) DO UPDATE
  SET authority_epoch=EXCLUDED.authority_epoch,active=TRUE;
-- Registration and producer-grant epochs are database-assigned on write by
-- the 259 bump guards: an INSERTed literal never sticks. Downstream pins
-- read back the assigned values instead of repeating the literals.
INSERT INTO scanner_advisory_producer_grants
  (principal_oid,collector_instance_id,advisory_scope_id,active)
VALUES (30,'fa-advisory','fa-advisory',TRUE);
INSERT INTO scanner_advisory_public_registrations
  (registration_id,collector_instance_id,advisory_scope_id,configuration_sha256,
   pinned_collector_instance_epoch,trusted_source_class,provider_id,content_profile,
   allowed_fact_kinds,max_age_seconds,active)
VALUES ('fa-reg','fa-advisory','fa-advisory',
        encode(sha256(convert_to((SELECT configuration::text FROM collector_instances WHERE instance_id='fa-advisory'),'UTF8')),'hex'),
        31,'vuln-intel','fa-provider','fa-profile','["vulnerability.cve"]'::jsonb,3600,TRUE);
INSERT INTO workflow_runs (run_id,trigger_kind,status,created_at,updated_at)
VALUES ('fa-run','schedule','collection_pending',now(),now());
INSERT INTO workflow_work_items
  (work_item_id,run_id,collector_kind,collector_instance_id,source_system,
   scope_id,acceptance_unit_id,source_run_id,generation_id,status,created_at,updated_at)
VALUES ('fa-awork','fa-run','vulnerability_intelligence','fa-advisory','vulnerability_intelligence',
        'fa-advisory','fa-advisory','fa-run','fa-adv-gen','claimed',now(),now());
INSERT INTO scanner_advisory_generation_receipts
  (receipt_id,registration_id,collector_instance_id,advisory_scope_id,advisory_generation_id,
   work_item_id,claim_id,claim_fencing_token,claim_owner_id,
   producer_principal_oid,pinned_producer_grant_epoch,pinned_registration_epoch,
   pinned_collector_instance_epoch,pinned_configuration_sha256,
   pinned_desired_revision,pinned_max_age_seconds,
   pinned_role_policy_revision,pinned_database_identity,
   fact_set_sha256,fact_count,status,published_at)
VALUES ('fa-arec','fa-reg','fa-advisory','fa-advisory','fa-adv-gen',
        'fa-awork','fa-aclaim',8,'fa-aowner',
        30,
        (SELECT grant_epoch FROM scanner_advisory_producer_grants
          WHERE principal_oid=30 AND collector_instance_id='fa-advisory' AND advisory_scope_id='fa-advisory'),
        (SELECT authority_epoch FROM scanner_advisory_public_registrations WHERE registration_id='fa-reg'),
        31,
        (SELECT configuration_sha256 FROM scanner_advisory_public_registrations WHERE registration_id='fa-reg'),
        1,3600,5,'55555555-5555-5555-5555-555555555555',
        'cc',1,'published',now());
INSERT INTO scanner_advisory_generation_facts
  (receipt_id,fact_id,fact_kind,payload_sha256)
SELECT 'fa-arec','fa-cve','vulnerability.cve',
       encode(sha256(convert_to(payload::text,'UTF8')),'hex')
FROM fact_records WHERE fact_id='fa-cve';
INSERT INTO scanner_advisory_public_activations
  (collector_instance_id,advisory_scope_id,active_generation_id,activation_epoch,active)
VALUES ('fa-advisory','fa-advisory','fa-adv-gen',34,TRUE);
INSERT INTO scanner_advisory_public_generations
  (registration_id,advisory_generation_id,approved_authority_epoch,pinned_advisory_scope_epoch,
   active,generation_receipt_id,producer_principal_oid,producer_grant_epoch,
   pinned_collector_instance_epoch,protected_activation_epoch,fact_set_sha256,
   approved_configuration_sha256,approved_observed_at,expires_at,approved_max_age_seconds,
   pinned_role_policy_revision,pinned_database_identity)
VALUES ('fa-reg','fa-adv-gen',
        (SELECT authority_epoch FROM scanner_advisory_public_registrations WHERE registration_id='fa-reg'),34,
        TRUE,'fa-arec',30,
        (SELECT grant_epoch FROM scanner_advisory_producer_grants
          WHERE principal_oid=30 AND collector_instance_id='fa-advisory' AND advisory_scope_id='fa-advisory'),
        31,34,'cc',
        (SELECT configuration_sha256 FROM scanner_advisory_public_registrations WHERE registration_id='fa-reg'),
        now()-interval '1 minute',now()+interval '59 minutes',3600,
        5,'55555555-5555-5555-5555-555555555555');
INSERT INTO scanner_image_result_advisory_dependencies
  (publication_id,authority_kind,authority_id,authority_epoch,
   advisory_scope_id,advisory_generation_id,advisory_scope_epoch,
   provider_id,trusted_source_class,content_profile)
VALUES ('fa-pub','public_registration','fa-reg',
        (SELECT authority_epoch FROM scanner_advisory_public_registrations WHERE registration_id='fa-reg'),
        'fa-advisory','fa-adv-gen',34,
        'fa-provider','vuln-intel','fa-profile');
`); err != nil {
		t.Fatalf("seed finding-authority advisory chain: %v", err)
	}
}

func checkFindingAuthorityDerivedReads(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	readIDs := func(caller auth.AuthContext, allowedScopes []string) []string {
		t.Helper()
		readCtx := auth.ContextWithAuthContext(ctx, caller)
		rows, err := NewPostgresFindingStore(database).ListSupplyChainImpactFindings(readCtx, FindingFilter{
			SubjectDigest:   "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			Limit:           200,
			AllowedScopeIDs: allowedScopes,
		})
		if err != nil {
			t.Fatalf("list findings: %v", err)
		}
		ids := []string{}
		for _, row := range rows {
			ids = append(ids, row.FindingID)
		}
		return ids
	}
	tenantA := auth.AuthContext{
		Mode: auth.AuthModeScoped, TenantID: "fa-tenant", WorkspaceID: "fa-workspace",
		SubjectClass: "collector", PolicyRevisionHash: "fa-policy", AllowedScopeIDs: []string{"fa-source"},
	}
	tenantB := auth.AuthContext{
		Mode: auth.AuthModeScoped, TenantID: "fa-tenant-b", WorkspaceID: "fa-workspace",
		SubjectClass: "collector", PolicyRevisionHash: "fa-policy", AllowedScopeIDs: []string{"fa-source"},
	}
	if ids := readIDs(tenantA, []string{"fa-source"}); len(ids) != 1 || ids[0] != "fa-finding-1" {
		t.Fatalf("scoped derived finding=%v, want [fa-finding-1]", ids)
	}
	if ids := readIDs(tenantB, []string{"fa-source"}); len(ids) != 0 {
		t.Fatalf("wrong-tenant derived finding=%v, want none", ids)
	}
	if ids := readIDs(tenantA, []string{"fa-source-other"}); len(ids) != 0 {
		t.Fatalf("wrong-scope derived finding=%v, want none", ids)
	}
	if _, err := database.ExecContext(ctx, `UPDATE tenant_scope_grants SET tombstoned_at=now()
WHERE tenant_id='fa-tenant' AND scope_id='fa-source'`); err != nil {
		t.Fatal(err)
	}
	if ids := readIDs(tenantA, []string{"fa-source"}); len(ids) != 0 {
		t.Fatalf("revoked derived finding=%v, want none", ids)
	}
}
