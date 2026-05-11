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
-- Locator hash table (precomputed against terraformstate.LocatorHash —
-- see go/internal/collector/terraformstate/identity.go:100). Regenerate by
-- running the helper in tests/fixtures/tfstate_drift/README.md if the hash
-- algorithm changes.
--
--   bucket=eshu-drift-a/prod/terraform.tfstate
--     hash=01c90e0b47d80dfc362bb2647d1306c5889b316d1c3b37ad91a49ccbb7e16ae5
--   bucket=eshu-drift-b/prod/terraform.tfstate
--     hash=ac3219927f950b4a0a57e415168765f0c49fccaa862b32e0906cacc29548eb66
--   bucket=eshu-drift-c/prod/terraform.tfstate
--     hash=33f0f3a35fa8bf42780910e64cc72a8977797dac9eecbbc50a623687bc79be38
--   bucket=eshu-drift-d/prod/terraform.tfstate
--     hash=6ef42db5dda508cb600e123a97ef2e128947bd582afac01e2e64a19dddf7ce4e
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
--
-- attribute_drift and removed_from_config are dormant in v1 (parser does not
-- emit per-attribute values; loader leaves PreviouslyDeclaredInConfig=false).
-- Tracked separately by issues #167 and #168.

BEGIN;

-- ----------------------------------------------------------------------------
-- 1. ingestion_scopes: one repo_snapshot scope per config-side repo
--    plus one state_snapshot scope per backend (A, B, C, D).
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
    ('state_snapshot:s3:01c90e0b47d80dfc362bb2647d1306c5889b316d1c3b37ad91a49ccbb7e16ae5',
     'state_snapshot', 'terraform_state', 'aws-s3:eshu-drift-a',
     'terraform_state', 'aws-s3:eshu-drift-a',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', 'gen:state:added-in-state'),

    ('state_snapshot:s3:ac3219927f950b4a0a57e415168765f0c49fccaa862b32e0906cacc29548eb66',
     'state_snapshot', 'terraform_state', 'aws-s3:eshu-drift-b',
     'terraform_state', 'aws-s3:eshu-drift-b',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', 'gen:state:added-in-config'),

    ('state_snapshot:s3:33f0f3a35fa8bf42780910e64cc72a8977797dac9eecbbc50a623687bc79be38',
     'state_snapshot', 'terraform_state', 'aws-s3:eshu-drift-c',
     'terraform_state', 'aws-s3:eshu-drift-c',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', 'gen:state:removed-from-state:current'),

    ('state_snapshot:s3:6ef42db5dda508cb600e123a97ef2e128947bd582afac01e2e64a19dddf7ce4e',
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
     'state_snapshot:s3:01c90e0b47d80dfc362bb2647d1306c5889b316d1c3b37ad91a49ccbb7e16ae5',
     'state_snapshot', 'lineage=lineage-A;serial=1',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', '2026-05-11T00:00:01Z'),

    ('gen:state:added-in-config',
     'state_snapshot:s3:ac3219927f950b4a0a57e415168765f0c49fccaa862b32e0906cacc29548eb66',
     'state_snapshot', 'lineage=lineage-B;serial=1',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', '2026-05-11T00:00:01Z'),

    ('gen:state:removed-from-state:prior',
     'state_snapshot:s3:33f0f3a35fa8bf42780910e64cc72a8977797dac9eecbbc50a623687bc79be38',
     'state_snapshot', 'lineage=lineage-C;serial=1',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'superseded', '2026-05-11T00:00:01Z'),

    ('gen:state:removed-from-state:current',
     'state_snapshot:s3:33f0f3a35fa8bf42780910e64cc72a8977797dac9eecbbc50a623687bc79be38',
     'state_snapshot', 'lineage=lineage-C;serial=2',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     'active', '2026-05-11T00:00:02Z'),

    ('gen:state:ambiguous',
     'state_snapshot:s3:6ef42db5dda508cb600e123a97ef2e128947bd582afac01e2e64a19dddf7ce4e',
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
     'state_snapshot:s3:01c90e0b47d80dfc362bb2647d1306c5889b316d1c3b37ad91a49ccbb7e16ae5',
     'gen:state:added-in-state',
     'terraform_state_snapshot', 'snapshot:01c90e0b',
     '1.0.0', 'terraform_state', 'terraform_state', 'snapshot:01c90e0b',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     jsonb_build_object(
        'lineage', 'lineage-A',
        'serial', 1,
        'backend_kind', 's3',
        'locator_hash', '01c90e0b47d80dfc362bb2647d1306c5889b316d1c3b37ad91a49ccbb7e16ae5'
     )),

    ('fact:snapshot:added-in-config',
     'state_snapshot:s3:ac3219927f950b4a0a57e415168765f0c49fccaa862b32e0906cacc29548eb66',
     'gen:state:added-in-config',
     'terraform_state_snapshot', 'snapshot:ac321992',
     '1.0.0', 'terraform_state', 'terraform_state', 'snapshot:ac321992',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     jsonb_build_object(
        'lineage', 'lineage-B',
        'serial', 1,
        'backend_kind', 's3',
        'locator_hash', 'ac3219927f950b4a0a57e415168765f0c49fccaa862b32e0906cacc29548eb66'
     )),

    -- Prior generation for scope C: serial=1, same lineage. Loader's
    -- priorStateSnapshotMetadataQuery looks up serial = currentSerial - 1.
    ('fact:snapshot:removed-from-state:prior',
     'state_snapshot:s3:33f0f3a35fa8bf42780910e64cc72a8977797dac9eecbbc50a623687bc79be38',
     'gen:state:removed-from-state:prior',
     'terraform_state_snapshot', 'snapshot:33f0f3a3:prior',
     '1.0.0', 'terraform_state', 'terraform_state', 'snapshot:33f0f3a3:prior',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     jsonb_build_object(
        'lineage', 'lineage-C',
        'serial', 1,
        'backend_kind', 's3',
        'locator_hash', '33f0f3a35fa8bf42780910e64cc72a8977797dac9eecbbc50a623687bc79be38'
     )),

    -- Active generation for scope C: serial=2, same lineage. Resource set is
    -- empty (the resource was removed between serials 1 and 2).
    ('fact:snapshot:removed-from-state:current',
     'state_snapshot:s3:33f0f3a35fa8bf42780910e64cc72a8977797dac9eecbbc50a623687bc79be38',
     'gen:state:removed-from-state:current',
     'terraform_state_snapshot', 'snapshot:33f0f3a3:current',
     '1.0.0', 'terraform_state', 'terraform_state', 'snapshot:33f0f3a3:current',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     jsonb_build_object(
        'lineage', 'lineage-C',
        'serial', 2,
        'backend_kind', 's3',
        'locator_hash', '33f0f3a35fa8bf42780910e64cc72a8977797dac9eecbbc50a623687bc79be38'
     )),

    ('fact:snapshot:ambiguous',
     'state_snapshot:s3:6ef42db5dda508cb600e123a97ef2e128947bd582afac01e2e64a19dddf7ce4e',
     'gen:state:ambiguous',
     'terraform_state_snapshot', 'snapshot:6ef42db5',
     '1.0.0', 'terraform_state', 'terraform_state', 'snapshot:6ef42db5',
     '2026-05-11T00:00:00Z', '2026-05-11T00:00:00Z',
     jsonb_build_object(
        'lineage', 'lineage-D',
        'serial', 1,
        'backend_kind', 's3',
        'locator_hash', '6ef42db5dda508cb600e123a97ef2e128947bd582afac01e2e64a19dddf7ce4e'
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
     'state_snapshot:s3:01c90e0b47d80dfc362bb2647d1306c5889b316d1c3b37ad91a49ccbb7e16ae5',
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
     'state_snapshot:s3:33f0f3a35fa8bf42780910e64cc72a8977797dac9eecbbc50a623687bc79be38',
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
