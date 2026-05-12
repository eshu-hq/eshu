-- tests/fixtures/tfstate_drift/seed.sql
--
-- Compose-level proof corpus for the Terraform config-vs-state drift handler
-- (issue #166, PR #165 follow-up). The script writes the exact rows the
-- production queries select against:
--
--   * go/internal/storage/postgres/tfstate_backend_canonical.go
--   * go/internal/storage/postgres/tfstate_drift_evidence.go
--   * go/internal/storage/postgres/drift_enqueue.go
--
-- Idempotent: every INSERT carries ON CONFLICT DO NOTHING. Safe to re-run
-- against the same Postgres without producing duplicates.
--
-- ============================================================================
-- Locator hash table (precomputed against terraformstate.ScopeLocatorHash —
-- see go/internal/collector/terraformstate/identity.go). The drift resolver
-- join key is intentionally version-agnostic; do NOT regenerate these with
-- terraformstate.LocatorHash, which carries VersionID. Regenerate by
-- running the helper in tests/fixtures/tfstate_drift/README.md if the hash
-- algorithm changes. (Issue #203 aligned the two hash functions for the
-- empty-VersionID case after a silent drift-rejection bug.)
--
--   bucket=eshu-drift-a/prod/terraform.tfstate
--     hash=92d2c3373ab7c1558170b1e20861a7aa3d53cbfde6545bc408b9972960d8af0f
--   bucket=eshu-drift-b/prod/terraform.tfstate
--     hash=0d25ca7f523acde9ee9afe3156e65992a34cbe3ec5ba7dd69db280f110aa1b01
--   bucket=eshu-drift-c/prod/terraform.tfstate
--     hash=84b08900a6df9339a8e1d1b258234e7da14f01c0761d9f78acb7658d1c120f9e
--   bucket=eshu-drift-d/prod/terraform.tfstate
--     hash=6815f7e3d49f4ed59c5975f9698aa1f9f7a52c0c8c8d4281f9da7c15cfc38c21
--   bucket=eshu-drift-e/prod/terraform.tfstate
--     hash=864eae7a3a2ae00d807f7566102a24b8b9910341f9d05f426d0501d5d2ce5cc9
--   bucket=eshu-drift-f/prod/terraform.tfstate
--     hash=76073467642da3783752710ac2dc36933140be5c4149ec8c66a58c3c280b12e5
-- ============================================================================
--
-- Scenarios and drift kinds (issue #166 in-scope set only):
--
--   bucket A → added_in_state         (state has resource, config does not)
--   bucket B → added_in_config        (config has resource, state does not)
--   bucket C → removed_from_state     (prior state has resource, current state
--                                      does not, config still declares it)
--   bucket D → ambiguous backend owner (two repos claim the same backend; the
--                                      resolver returns ErrAmbiguousBackendOwner
--                                      and the handler logs a WARN rejection
--                                      with failure_class="ambiguous_backend_owner".
--                                      No drift counter increment.)
--   bucket E → attribute_drift          (both sides declare aws_s3_bucket.logs;
--                                       config SSE algorithm AES256, state aws:kms.
--                                       Exercises the deepest nested allowlist
--                                       path end-to-end.)
--   bucket F → removed_from_config     (state has aws_iam_policy.legacy;
--                                      current repo generation does not
--                                      declare it; prior (superseded) repo
--                                      generation did. Exercises the
--                                      prior-config walk end-to-end.)

BEGIN;

-- ----------------------------------------------------------------------------
-- 1. ingestion_scopes: one repo_snapshot scope per config-side repo
--    plus one state_snapshot scope per backend (A, B, C, D, E, F).
-- ----------------------------------------------------------------------------

INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES
    ('repo_snapshot:drift-tfstate-added-in-state',
     'repo_snapshot', 'git', 'drift-tfstate-added-in-state', 'git',
     'drift-tfstate-added-in-state',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', 'gen:repo:drift-tfstate-added-in-state'),

    ('repo_snapshot:drift-tfstate-added-in-config',
     'repo_snapshot', 'git', 'drift-tfstate-added-in-config', 'git',
     'drift-tfstate-added-in-config',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', 'gen:repo:drift-tfstate-added-in-config'),

    ('repo_snapshot:drift-tfstate-removed-from-state',
     'repo_snapshot', 'git', 'drift-tfstate-removed-from-state', 'git',
     'drift-tfstate-removed-from-state',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', 'gen:repo:drift-tfstate-removed-from-state'),

    ('repo_snapshot:drift-tfstate-ambiguous-a',
     'repo_snapshot', 'git', 'drift-tfstate-ambiguous-a', 'git',
     'drift-tfstate-ambiguous-a',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', 'gen:repo:drift-tfstate-ambiguous-a'),

    ('repo_snapshot:drift-tfstate-ambiguous-b',
     'repo_snapshot', 'git', 'drift-tfstate-ambiguous-b', 'git',
     'drift-tfstate-ambiguous-b',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', 'gen:repo:drift-tfstate-ambiguous-b'),

    -- state_snapshot:<backend_kind>:<locator_hash> per scope/tfstate.go:33-40.
    ('state_snapshot:s3:92d2c3373ab7c1558170b1e20861a7aa3d53cbfde6545bc408b9972960d8af0f',
     'state_snapshot', 'terraform_state', 'aws-s3:eshu-drift-a',
     'terraform_state', 'aws-s3:eshu-drift-a',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', 'gen:state:added-in-state'),

    ('state_snapshot:s3:0d25ca7f523acde9ee9afe3156e65992a34cbe3ec5ba7dd69db280f110aa1b01',
     'state_snapshot', 'terraform_state', 'aws-s3:eshu-drift-b',
     'terraform_state', 'aws-s3:eshu-drift-b',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', 'gen:state:added-in-config'),

    ('state_snapshot:s3:84b08900a6df9339a8e1d1b258234e7da14f01c0761d9f78acb7658d1c120f9e',
     'state_snapshot', 'terraform_state', 'aws-s3:eshu-drift-c',
     'terraform_state', 'aws-s3:eshu-drift-c',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', 'gen:state:removed-from-state:current'),

    ('state_snapshot:s3:6815f7e3d49f4ed59c5975f9698aa1f9f7a52c0c8c8d4281f9da7c15cfc38c21',
     'state_snapshot', 'terraform_state', 'aws-s3:eshu-drift-d',
     'terraform_state', 'aws-s3:eshu-drift-d',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', 'gen:state:ambiguous')
ON CONFLICT (scope_id) DO NOTHING;

-- ----------------------------------------------------------------------------
-- 2. scope_generations: active generation per scope; plus the superseded
--    prior generation for the removed_from_state state_snapshot.
-- ----------------------------------------------------------------------------

INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at
) VALUES
    ('gen:repo:drift-tfstate-added-in-state',
     'repo_snapshot:drift-tfstate-added-in-state',
     'commit', NULL,
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', '2026-05-11T00:00:01Z'),

    ('gen:repo:drift-tfstate-added-in-config',
     'repo_snapshot:drift-tfstate-added-in-config',
     'commit', NULL,
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', '2026-05-11T00:00:01Z'),

    ('gen:repo:drift-tfstate-removed-from-state',
     'repo_snapshot:drift-tfstate-removed-from-state',
     'commit', NULL,
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', '2026-05-11T00:00:01Z'),

    ('gen:repo:drift-tfstate-ambiguous-a',
     'repo_snapshot:drift-tfstate-ambiguous-a',
     'commit', NULL,
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', '2026-05-11T00:00:01Z'),

    ('gen:repo:drift-tfstate-ambiguous-b',
     'repo_snapshot:drift-tfstate-ambiguous-b',
     'commit', NULL,
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', '2026-05-11T00:00:01Z'),

    ('gen:state:added-in-state',
     'state_snapshot:s3:92d2c3373ab7c1558170b1e20861a7aa3d53cbfde6545bc408b9972960d8af0f',
     'state_snapshot', 'lineage=lineage-A;serial=1',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', '2026-05-11T00:00:01Z'),

    ('gen:state:added-in-config',
     'state_snapshot:s3:0d25ca7f523acde9ee9afe3156e65992a34cbe3ec5ba7dd69db280f110aa1b01',
     'state_snapshot', 'lineage=lineage-B;serial=1',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', '2026-05-11T00:00:01Z'),

    ('gen:state:removed-from-state:prior',
     'state_snapshot:s3:84b08900a6df9339a8e1d1b258234e7da14f01c0761d9f78acb7658d1c120f9e',
     'state_snapshot', 'lineage=lineage-C;serial=1',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'superseded', '2026-05-11T00:00:01Z'),

    ('gen:state:removed-from-state:current',
     'state_snapshot:s3:84b08900a6df9339a8e1d1b258234e7da14f01c0761d9f78acb7658d1c120f9e',
     'state_snapshot', 'lineage=lineage-C;serial=2',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', '2026-05-11T00:00:02Z'),

    ('gen:state:ambiguous',
     'state_snapshot:s3:6815f7e3d49f4ed59c5975f9698aa1f9f7a52c0c8c8d4281f9da7c15cfc38c21',
     'state_snapshot', 'lineage=lineage-D;serial=1',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', '2026-05-11T00:00:01Z')
ON CONFLICT (generation_id) DO NOTHING;

-- ----------------------------------------------------------------------------
-- 3. fact_records — config-side `file` facts carrying terraform_backends and
--    optionally terraform_resources arrays. Matches the parser-fact payload
--    shape consumed by:
--      * listTerraformBackendCanonicalRowsQuery
--      * listConfigResourcesForCommitQuery
--    The `_is_literal` flags satisfy isExactBackendAttribute checks at
--    tfstate_backend_facts.go:512-538.
-- ----------------------------------------------------------------------------

-- Scenario A: drift_added_in_state — config has the backend block only; no
-- resource block for the address the state will later list. State will carry
-- aws_s3_bucket.unmanaged, config has no aws_s3_bucket.* declaration.
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_system, source_fact_key,
    observed_at, ingested_at, payload
) VALUES (
    'fact:drift-added-in-state:main.tf',
    'repo_snapshot:drift-tfstate-added-in-state',
    'gen:repo:drift-tfstate-added-in-state',
    'file', 'drift-tfstate-added-in-state:main.tf',
    '1.0.0', 'git', 'git', 'drift-tfstate-added-in-state:main.tf',
    '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
    jsonb_build_object(
        'repo_id', 'drift-tfstate-added-in-state',
        'relative_path', 'main.tf',
        'parsed_file_data', jsonb_build_object(
            'terraform_backends', jsonb_build_array(jsonb_build_object(
                'backend_kind', 's3',
                'bucket', 'eshu-drift-a',
                'bucket_is_literal', true,
                'key', 'prod/terraform.tfstate',
                'key_is_literal', true,
                'region', 'us-east-1',
                'region_is_literal', true
            )),
            'terraform_resources', '[]'::jsonb
        )
    )
)
ON CONFLICT (fact_id) DO NOTHING;

-- Scenario B: drift_added_in_config — config declares aws_s3_bucket.declared;
-- state will have zero terraform_state_resource rows.
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_system, source_fact_key,
    observed_at, ingested_at, payload
) VALUES (
    'fact:drift-added-in-config:main.tf',
    'repo_snapshot:drift-tfstate-added-in-config',
    'gen:repo:drift-tfstate-added-in-config',
    'file', 'drift-tfstate-added-in-config:main.tf',
    '1.0.0', 'git', 'git', 'drift-tfstate-added-in-config:main.tf',
    '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
    jsonb_build_object(
        'repo_id', 'drift-tfstate-added-in-config',
        'relative_path', 'main.tf',
        'parsed_file_data', jsonb_build_object(
            'terraform_backends', jsonb_build_array(jsonb_build_object(
                'backend_kind', 's3',
                'bucket', 'eshu-drift-b',
                'bucket_is_literal', true,
                'key', 'prod/terraform.tfstate',
                'key_is_literal', true,
                'region', 'us-east-1',
                'region_is_literal', true
            )),
            'terraform_resources', jsonb_build_array(jsonb_build_object(
                'resource_type', 'aws_s3_bucket',
                'resource_name', 'declared'
            ))
        )
    )
)
ON CONFLICT (fact_id) DO NOTHING;

-- Scenario C: drift_removed_from_state — config declares aws_s3_bucket.was_there;
-- prior state had it, current state does not. Same lineage to avoid
-- LineageRotation suppression.
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_system, source_fact_key,
    observed_at, ingested_at, payload
) VALUES (
    'fact:drift-removed-from-state:main.tf',
    'repo_snapshot:drift-tfstate-removed-from-state',
    'gen:repo:drift-tfstate-removed-from-state',
    'file', 'drift-tfstate-removed-from-state:main.tf',
    '1.0.0', 'git', 'git', 'drift-tfstate-removed-from-state:main.tf',
    '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
    jsonb_build_object(
        'repo_id', 'drift-tfstate-removed-from-state',
        'relative_path', 'main.tf',
        'parsed_file_data', jsonb_build_object(
            'terraform_backends', jsonb_build_array(jsonb_build_object(
                'backend_kind', 's3',
                'bucket', 'eshu-drift-c',
                'bucket_is_literal', true,
                'key', 'prod/terraform.tfstate',
                'key_is_literal', true,
                'region', 'us-east-1',
                'region_is_literal', true
            )),
            'terraform_resources', jsonb_build_array(jsonb_build_object(
                'resource_type', 'aws_s3_bucket',
                'resource_name', 'was_there'
            ))
        )
    )
)
ON CONFLICT (fact_id) DO NOTHING;

-- Scenario D: ambiguous-owner pair — two repos both declare the same
-- (backend_kind, locator_hash). The canonical adapter returns rows from
-- both repos; the resolver sees len(unique RepoID) > 1 and returns
-- ErrAmbiguousBackendOwner.
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_system, source_fact_key,
    observed_at, ingested_at, payload
) VALUES
    ('fact:drift-ambiguous-a:main.tf',
     'repo_snapshot:drift-tfstate-ambiguous-a',
     'gen:repo:drift-tfstate-ambiguous-a',
     'file', 'drift-tfstate-ambiguous-a:main.tf',
     '1.0.0', 'git', 'git', 'drift-tfstate-ambiguous-a:main.tf',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     jsonb_build_object(
        'repo_id', 'drift-tfstate-ambiguous-a',
        'relative_path', 'main.tf',
        'parsed_file_data', jsonb_build_object(
            'terraform_backends', jsonb_build_array(jsonb_build_object(
                'backend_kind', 's3',
                'bucket', 'eshu-drift-d',
                'bucket_is_literal', true,
                'key', 'prod/terraform.tfstate',
                'key_is_literal', true,
                'region', 'us-east-1',
                'region_is_literal', true
            )),
            'terraform_resources', '[]'::jsonb
        ))),
    ('fact:drift-ambiguous-b:main.tf',
     'repo_snapshot:drift-tfstate-ambiguous-b',
     'gen:repo:drift-tfstate-ambiguous-b',
     'file', 'drift-tfstate-ambiguous-b:main.tf',
     '1.0.0', 'git', 'git', 'drift-tfstate-ambiguous-b:main.tf',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     jsonb_build_object(
        'repo_id', 'drift-tfstate-ambiguous-b',
        'relative_path', 'main.tf',
        'parsed_file_data', jsonb_build_object(
            'terraform_backends', jsonb_build_array(jsonb_build_object(
                'backend_kind', 's3',
                'bucket', 'eshu-drift-d',
                'bucket_is_literal', true,
                'key', 'prod/terraform.tfstate',
                'key_is_literal', true,
                'region', 'us-east-1',
                'region_is_literal', true
            )),
            'terraform_resources', '[]'::jsonb
        )))
ON CONFLICT (fact_id) DO NOTHING;

-- ----------------------------------------------------------------------------
-- 4. fact_records — state-side `terraform_state_snapshot` facts. One per
--    active state generation, plus the prior generation for scope C.
--    Payload carries lineage/serial as activeStateSnapshotMetadataQuery and
--    priorStateSnapshotMetadataQuery read them.
-- ----------------------------------------------------------------------------

INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_system, source_fact_key,
    observed_at, ingested_at, payload
) VALUES
    ('fact:snapshot:added-in-state',
     'state_snapshot:s3:92d2c3373ab7c1558170b1e20861a7aa3d53cbfde6545bc408b9972960d8af0f',
     'gen:state:added-in-state',
     'terraform_state_snapshot', 'snapshot:92d2c337',
     '1.0.0', 'terraform_state', 'terraform_state', 'snapshot:92d2c337',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     jsonb_build_object(
        'lineage', 'lineage-A',
        'serial', 1,
        'backend_kind', 's3',
        'locator_hash', '92d2c3373ab7c1558170b1e20861a7aa3d53cbfde6545bc408b9972960d8af0f'
     )),

    ('fact:snapshot:added-in-config',
     'state_snapshot:s3:0d25ca7f523acde9ee9afe3156e65992a34cbe3ec5ba7dd69db280f110aa1b01',
     'gen:state:added-in-config',
     'terraform_state_snapshot', 'snapshot:0d25ca7f',
     '1.0.0', 'terraform_state', 'terraform_state', 'snapshot:0d25ca7f',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     jsonb_build_object(
        'lineage', 'lineage-B',
        'serial', 1,
        'backend_kind', 's3',
        'locator_hash', '0d25ca7f523acde9ee9afe3156e65992a34cbe3ec5ba7dd69db280f110aa1b01'
     )),

    -- Prior generation for scope C: serial=1, same lineage. Loader's
    -- priorStateSnapshotMetadataQuery looks up serial = currentSerial - 1.
    ('fact:snapshot:removed-from-state:prior',
     'state_snapshot:s3:84b08900a6df9339a8e1d1b258234e7da14f01c0761d9f78acb7658d1c120f9e',
     'gen:state:removed-from-state:prior',
     'terraform_state_snapshot', 'snapshot:84b08900:prior',
     '1.0.0', 'terraform_state', 'terraform_state', 'snapshot:84b08900:prior',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     jsonb_build_object(
        'lineage', 'lineage-C',
        'serial', 1,
        'backend_kind', 's3',
        'locator_hash', '84b08900a6df9339a8e1d1b258234e7da14f01c0761d9f78acb7658d1c120f9e'
     )),

    -- Active generation for scope C: serial=2, same lineage. Resource set is
    -- empty (the resource was removed between serials 1 and 2).
    ('fact:snapshot:removed-from-state:current',
     'state_snapshot:s3:84b08900a6df9339a8e1d1b258234e7da14f01c0761d9f78acb7658d1c120f9e',
     'gen:state:removed-from-state:current',
     'terraform_state_snapshot', 'snapshot:84b08900:current',
     '1.0.0', 'terraform_state', 'terraform_state', 'snapshot:84b08900:current',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     jsonb_build_object(
        'lineage', 'lineage-C',
        'serial', 2,
        'backend_kind', 's3',
        'locator_hash', '84b08900a6df9339a8e1d1b258234e7da14f01c0761d9f78acb7658d1c120f9e'
     )),

    ('fact:snapshot:ambiguous',
     'state_snapshot:s3:6815f7e3d49f4ed59c5975f9698aa1f9f7a52c0c8c8d4281f9da7c15cfc38c21',
     'gen:state:ambiguous',
     'terraform_state_snapshot', 'snapshot:6815f7e3',
     '1.0.0', 'terraform_state', 'terraform_state', 'snapshot:6815f7e3',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     jsonb_build_object(
        'lineage', 'lineage-D',
        'serial', 1,
        'backend_kind', 's3',
        'locator_hash', '6815f7e3d49f4ed59c5975f9698aa1f9f7a52c0c8c8d4281f9da7c15cfc38c21'
     ))
ON CONFLICT (fact_id) DO NOTHING;

-- ----------------------------------------------------------------------------
-- 5. fact_records — state-side `terraform_state_resource` facts. One per
--    address present in each generation. Payload carries `address` and
--    `type` per listStateResourcesForGenerationQuery (and the loader's
--    stateRowFromCollectorPayload decoder).
-- ----------------------------------------------------------------------------

INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_system, source_fact_key,
    observed_at, ingested_at, payload
) VALUES
    -- Scenario A: state carries aws_s3_bucket.unmanaged; config has nothing.
    ('fact:resource:added-in-state:unmanaged',
     'state_snapshot:s3:92d2c3373ab7c1558170b1e20861a7aa3d53cbfde6545bc408b9972960d8af0f',
     'gen:state:added-in-state',
     'terraform_state_resource', 'resource:added-in-state:unmanaged',
     '1.0.0', 'terraform_state', 'terraform_state', 'resource:added-in-state:unmanaged',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     jsonb_build_object(
        'address', 'aws_s3_bucket.unmanaged',
        'type', 'aws_s3_bucket',
        'attributes', jsonb_build_object()
     )),

    -- Scenario C prior generation: state carries aws_s3_bucket.was_there.
    ('fact:resource:removed-from-state:prior:was_there',
     'state_snapshot:s3:84b08900a6df9339a8e1d1b258234e7da14f01c0761d9f78acb7658d1c120f9e',
     'gen:state:removed-from-state:prior',
     'terraform_state_resource', 'resource:removed-from-state:prior:was_there',
     '1.0.0', 'terraform_state', 'terraform_state', 'resource:removed-from-state:prior:was_there',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     jsonb_build_object(
        'address', 'aws_s3_bucket.was_there',
        'type', 'aws_s3_bucket',
        'attributes', jsonb_build_object()
     ))

    -- Scenario B (added_in_config) deliberately has zero terraform_state_resource
    -- rows under gen:state:added-in-config. Scenario C current generation
    -- deliberately has zero rows under gen:state:removed-from-state:current.
    -- Scenario D (ambiguous) does not need any state resources — the handler
    -- bails out at the resolver step before reaching the evidence loader.

ON CONFLICT (fact_id) DO NOTHING;

-- ----------------------------------------------------------------------------
-- Bucket E: attribute_drift — aws_s3_bucket.logs present on both sides;
-- config SSE algorithm is AES256, state SSE algorithm is aws:kms. The acl
-- value matches on both sides (private) to confirm only the differing key
-- fires. The state payload uses the nested singleton-array shape so that
-- flattenStateAttributes exercises the deepest singleton-array unwrap.
-- ----------------------------------------------------------------------------

-- 1. ingestion_scopes: one repo_snapshot + one state_snapshot for bucket E.
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES
    ('repo_snapshot:drift-tfstate-attribute-drift',
     'repo_snapshot', 'git', 'drift-tfstate-attribute-drift', 'git',
     'drift-tfstate-attribute-drift',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', 'gen:repo:drift-tfstate-attribute-drift'),

    ('state_snapshot:s3:864eae7a3a2ae00d807f7566102a24b8b9910341f9d05f426d0501d5d2ce5cc9',
     'state_snapshot', 'terraform_state', 'aws-s3:eshu-drift-e',
     'terraform_state', 'aws-s3:eshu-drift-e',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', 'gen:state:attribute-drift')
ON CONFLICT (scope_id) DO NOTHING;

-- 2. scope_generations: one active generation per scope.
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at
) VALUES
    ('gen:repo:drift-tfstate-attribute-drift',
     'repo_snapshot:drift-tfstate-attribute-drift',
     'commit', NULL,
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', '2026-05-11T00:00:01Z'),

    ('gen:state:attribute-drift',
     'state_snapshot:s3:864eae7a3a2ae00d807f7566102a24b8b9910341f9d05f426d0501d5d2ce5cc9',
     'state_snapshot', 'lineage=lineage-E;serial=1',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', '2026-05-11T00:00:01Z')
ON CONFLICT (generation_id) DO NOTHING;

-- 3. fact_records — config-side file fact carrying terraform_backends and
--    terraform_resources. The resources entry carries a flat dot-path
--    attributes map matching the parser's HCL attribute encoder output.
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_system, source_fact_key,
    observed_at, ingested_at, payload
) VALUES (
    'fact:drift-attribute-drift:main.tf',
    'repo_snapshot:drift-tfstate-attribute-drift',
    'gen:repo:drift-tfstate-attribute-drift',
    'file', 'drift-tfstate-attribute-drift:main.tf',
    '1.0.0', 'git', 'git', 'drift-tfstate-attribute-drift:main.tf',
    '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
    jsonb_build_object(
        'repo_id', 'drift-tfstate-attribute-drift',
        'relative_path', 'main.tf',
        'parsed_file_data', jsonb_build_object(
            'terraform_backends', jsonb_build_array(jsonb_build_object(
                'backend_kind', 's3',
                'bucket', 'eshu-drift-e',
                'bucket_is_literal', true,
                'key', 'prod/terraform.tfstate',
                'key_is_literal', true,
                'region', 'us-east-1',
                'region_is_literal', true
            )),
            'terraform_resources', jsonb_build_array(jsonb_build_object(
                'resource_type', 'aws_s3_bucket',
                'resource_name', 'logs',
                'name', 'logs',
                'path', 'main.tf',
                'lang', 'hcl',
                'line_number', 1,
                'attributes', jsonb_build_object(
                    'acl', 'private',
                    'server_side_encryption_configuration.rule.apply_server_side_encryption_by_default.sse_algorithm', 'AES256'
                )
            ))
        )
    )
)
ON CONFLICT (fact_id) DO NOTHING;

-- 4. fact_records — state-side terraform_state_snapshot fact.
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_system, source_fact_key,
    observed_at, ingested_at, payload
) VALUES (
    'fact:snapshot:attribute-drift',
    'state_snapshot:s3:864eae7a3a2ae00d807f7566102a24b8b9910341f9d05f426d0501d5d2ce5cc9',
    'gen:state:attribute-drift',
    'terraform_state_snapshot', 'snapshot:864eae7a',
    '1.0.0', 'terraform_state', 'terraform_state', 'snapshot:864eae7a',
    '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
    jsonb_build_object(
        'lineage', 'lineage-E',
        'serial', 1,
        'backend_kind', 's3',
        'locator_hash', '864eae7a3a2ae00d807f7566102a24b8b9910341f9d05f426d0501d5d2ce5cc9'
    )
)
ON CONFLICT (fact_id) DO NOTHING;

-- 5. fact_records — state-side terraform_state_resource fact. The attributes
--    use the nested singleton-array shape the collector emits so that
--    flattenStateAttributes exercises the deep SSE unwrap. The acl value
--    matches the config side (private) — only the SSE path drifts.
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_system, source_fact_key,
    observed_at, ingested_at, payload
) VALUES (
    'fact:resource:attribute-drift:logs',
    'state_snapshot:s3:864eae7a3a2ae00d807f7566102a24b8b9910341f9d05f426d0501d5d2ce5cc9',
    'gen:state:attribute-drift',
    'terraform_state_resource', 'resource:attribute-drift:logs',
    '1.0.0', 'terraform_state', 'terraform_state', 'resource:attribute-drift:logs',
    '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
    jsonb_build_object(
        'address', 'aws_s3_bucket.logs',
        'type', 'aws_s3_bucket',
        'name', 'logs',
        'attributes', jsonb_build_object(
            'acl', 'private',
            'server_side_encryption_configuration', jsonb_build_array(
                jsonb_build_object(
                    'rule', jsonb_build_array(
                        jsonb_build_object(
                            'apply_server_side_encryption_by_default', jsonb_build_array(
                                jsonb_build_object(
                                    'sse_algorithm', 'aws:kms'
                                )
                            )
                        )
                    )
                )
            )
        )
    )
)
ON CONFLICT (fact_id) DO NOTHING;

-- ----------------------------------------------------------------------------
-- Bucket F: removed_from_config — aws_iam_policy.legacy is present in state
-- and in the prior (superseded) repo generation but absent from the current
-- (active) repo generation. The prior-config walk
-- (listPriorConfigAddressesQuery) finds the address in the superseded
-- generation and sets PreviouslyDeclaredInConfig=true so the classifier emits
-- removed_from_config rather than added_in_state.
--
-- Two repo-snapshot generations share the same scope_id:
--   gen:repo:drift-tfstate-removed-from-config-2  status=active  (current)
--   gen:repo:drift-tfstate-removed-from-config-1  status=superseded (prior)
--
-- The prior (superseded) generation has an earlier ingested_at than the
-- active one, so the ORDER BY ingested_at DESC LIMIT N walk in
-- listPriorConfigAddressesQuery (after excluding the current generation_id
-- via $2) returns the prior generation when iterating newest-to-oldest. The active
-- ingestion_scopes row points at generation-2, so the canonical backend query
-- resolves to generation-2 (which has the backend block but no
-- aws_iam_policy.legacy resource).
-- ----------------------------------------------------------------------------

-- 1. ingestion_scopes: one repo_snapshot + one state_snapshot for bucket F.
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES
    ('repo_snapshot:drift-tfstate-removed-from-config',
     'repo_snapshot', 'git', 'drift-tfstate-removed-from-config', 'git',
     'drift-tfstate-removed-from-config',
     '2026-05-11T00:00:01Z', '2026-05-11T00:00:01Z',
     'active', 'gen:repo:drift-tfstate-removed-from-config-2'),

    ('state_snapshot:s3:76073467642da3783752710ac2dc36933140be5c4149ec8c66a58c3c280b12e5',
     'state_snapshot', 'terraform_state', 'aws-s3:eshu-drift-f',
     'terraform_state', 'aws-s3:eshu-drift-f',
     '2026-05-11T00:00:01Z', '2026-05-11T00:00:01Z',
     'active', 'gen:state:removed-from-config')
ON CONFLICT (scope_id) DO NOTHING;

-- 2. scope_generations: current (active) + prior (superseded) for the repo
--    scope, plus one active generation for the state scope.
--    The prior repo generation MUST have an earlier ingested_at than the
--    current generation so that listPriorConfigAddressesQuery's
--    ORDER BY ingested_at DESC places the prior generation in the walk window.
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at
) VALUES
    -- Current repo generation: active, ingested after the prior one.
    ('gen:repo:drift-tfstate-removed-from-config-2',
     'repo_snapshot:drift-tfstate-removed-from-config',
     'commit', NULL,
     '2026-05-11T00:00:01Z', '2026-05-11T00:00:01Z',
     'active', '2026-05-11T00:00:01Z'),

    -- Prior repo generation: superseded, ingested before the current one.
    -- The earlier ingested_at is load-bearing: the prior-config walk
    -- orders by ingested_at DESC and the current generation_id is excluded
    -- ($2 filter), so this row is first in the walk result.
    ('gen:repo:drift-tfstate-removed-from-config-1',
     'repo_snapshot:drift-tfstate-removed-from-config',
     'commit', NULL,
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'superseded', '2026-05-11T00:00:01Z'),

    -- State generation: active, serial=1.
    ('gen:state:removed-from-config',
     'state_snapshot:s3:76073467642da3783752710ac2dc36933140be5c4149ec8c66a58c3c280b12e5',
     'state_snapshot', 'lineage=lineage-F;serial=1',
     '2026-05-11T00:00:01Z', '2026-05-11T00:00:01Z',
     'active', '2026-05-11T00:00:01Z')
ON CONFLICT (generation_id) DO NOTHING;

-- 3. fact_records — config-side file facts for both repo generations.
--    Current generation (gen-2): has the backend block but NO
--    aws_iam_policy.legacy resource. An unrelated resource
--    (aws_s3_bucket.unrelated) is present to prove the file was indexed.
--    Side effect: aws_s3_bucket.unrelated is config-only and produces an
--    added_in_config drift event. The verify script's counter assertion
--    uses delta>=1 so bucket-F contributes one removed_from_config and one
--    added_in_config; the bucket-B added_in_config still fires too, bringing
--    the total to delta=2 for added_in_config across the run.
--    Prior generation (gen-1): has the same backend block AND
--    aws_iam_policy.legacy, which is the address the prior-config walk must
--    find to promote it to removed_from_config.

-- Current repo generation — no aws_iam_policy.legacy.
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_system, source_fact_key,
    observed_at, ingested_at, payload
) VALUES (
    'fact:drift-removed-from-config:current:main.tf',
    'repo_snapshot:drift-tfstate-removed-from-config',
    'gen:repo:drift-tfstate-removed-from-config-2',
    'file', 'drift-tfstate-removed-from-config:main.tf',
    '1.0.0', 'git', 'git', 'drift-tfstate-removed-from-config:main.tf',
    '2026-05-11T00:00:01Z', '2026-05-11T00:00:01Z',
    jsonb_build_object(
        'repo_id', 'drift-tfstate-removed-from-config',
        'relative_path', 'main.tf',
        'parsed_file_data', jsonb_build_object(
            'terraform_backends', jsonb_build_array(jsonb_build_object(
                'backend_kind', 's3',
                'bucket', 'eshu-drift-f',
                'bucket_is_literal', true,
                'key', 'prod/terraform.tfstate',
                'key_is_literal', true,
                'region', 'us-east-1',
                'region_is_literal', true
            )),
            'terraform_resources', jsonb_build_array(jsonb_build_object(
                'resource_type', 'aws_s3_bucket',
                'resource_name', 'unrelated'
            ))
        )
    )
)
ON CONFLICT (fact_id) DO NOTHING;

-- Prior repo generation — declares aws_iam_policy.legacy.
-- The same backend block is required so both generations share the backend
-- identity; the locator hash is identical because bucket+key are unchanged.
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_system, source_fact_key,
    observed_at, ingested_at, payload
) VALUES (
    'fact:drift-removed-from-config:prior:main.tf',
    'repo_snapshot:drift-tfstate-removed-from-config',
    'gen:repo:drift-tfstate-removed-from-config-1',
    'file', 'drift-tfstate-removed-from-config:prior:main.tf',
    '1.0.0', 'git', 'git', 'drift-tfstate-removed-from-config:prior:main.tf',
    '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
    jsonb_build_object(
        'repo_id', 'drift-tfstate-removed-from-config',
        'relative_path', 'main.tf',
        'parsed_file_data', jsonb_build_object(
            'terraform_backends', jsonb_build_array(jsonb_build_object(
                'backend_kind', 's3',
                'bucket', 'eshu-drift-f',
                'bucket_is_literal', true,
                'key', 'prod/terraform.tfstate',
                'key_is_literal', true,
                'region', 'us-east-1',
                'region_is_literal', true
            )),
            'terraform_resources', jsonb_build_array(jsonb_build_object(
                'resource_type', 'aws_iam_policy',
                'resource_name', 'legacy'
            ))
        )
    )
)
ON CONFLICT (fact_id) DO NOTHING;

-- 4. fact_records — state-side terraform_state_snapshot fact for bucket F.
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_system, source_fact_key,
    observed_at, ingested_at, payload
) VALUES (
    'fact:snapshot:removed-from-config',
    'state_snapshot:s3:76073467642da3783752710ac2dc36933140be5c4149ec8c66a58c3c280b12e5',
    'gen:state:removed-from-config',
    'terraform_state_snapshot', 'snapshot:76073467',
    '1.0.0', 'terraform_state', 'terraform_state', 'snapshot:76073467',
    '2026-05-11T00:00:01Z', '2026-05-11T00:00:01Z',
    jsonb_build_object(
        'lineage', 'lineage-F',
        'serial', 1,
        'backend_kind', 's3',
        'locator_hash', '76073467642da3783752710ac2dc36933140be5c4149ec8c66a58c3c280b12e5'
    )
)
ON CONFLICT (fact_id) DO NOTHING;

-- 5. fact_records — state-side terraform_state_resource fact for bucket F.
--    State carries aws_iam_policy.legacy; the current config generation does
--    not — the prior-config walk is the only signal that the address was
--    previously managed.
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_system, source_fact_key,
    observed_at, ingested_at, payload
) VALUES (
    'fact:resource:removed-from-config:legacy',
    'state_snapshot:s3:76073467642da3783752710ac2dc36933140be5c4149ec8c66a58c3c280b12e5',
    'gen:state:removed-from-config',
    'terraform_state_resource', 'resource:removed-from-config:legacy',
    '1.0.0', 'terraform_state', 'terraform_state', 'resource:removed-from-config:legacy',
    '2026-05-11T00:00:01Z', '2026-05-11T00:00:01Z',
    jsonb_build_object(
        'address', 'aws_iam_policy.legacy',
        'type', 'aws_iam_policy',
        'attributes', jsonb_build_object()
    )
)
ON CONFLICT (fact_id) DO NOTHING;

COMMIT;

-- ----------------------------------------------------------------------------
-- Verification queries (informational; not executed by the verifier script).
-- Run these manually after seeding to sanity-check the corpus:
--
--   SELECT scope_id FROM ingestion_scopes WHERE scope_id LIKE 'state_snapshot:%' ORDER BY scope_id;
--   SELECT scope_id, generation_id, status FROM scope_generations
--     WHERE scope_id LIKE 'state_snapshot:%' ORDER BY scope_id, status;
--   SELECT scope_id, fact_kind, payload->>'address'
--     FROM fact_records
--     WHERE fact_kind IN ('terraform_state_snapshot', 'terraform_state_resource')
--     ORDER BY scope_id, fact_kind;
-- ----------------------------------------------------------------------------
