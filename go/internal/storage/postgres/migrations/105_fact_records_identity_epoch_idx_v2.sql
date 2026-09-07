-- 105_fact_records_identity_epoch_idx_v2.sql
--
-- The container-image-identity epoch/load partial index (#5438), renamed to the
-- _v2 suffix by #6543 and carrying the predicate #5460 widened it to: the arm
-- admitting Dockerfile base images. A Dockerfile is never a content_entity --
-- the parser emits it as a `file` fact whose parsed_file_data carries
-- dockerfile_stages (the base image FROM refs) -- so identityFactFilterSQL
-- (facts_active_container_image_identity.go) has a `file` arm, and a partial
-- index only covers a query when its predicate is a subset match of the query's
-- filter. The narrower pre-#5460 predicate would silently drop the epoch probe
-- and the identity fact load back to a full scan, which is why the predicate
-- below is the one that survives and why
-- TestIdentityEpochIndexPredicateMatchesIdentityFactFilter reads THIS file to
-- drift-lock it against the Go const.
--
-- Nothing is lost by dropping the older definition. Migration 077's predicate
-- was migration 069's verbatim plus the `file`/dockerfile_stages arm alone, on
-- the same (observed_at, fact_id) key and with the same
-- `AND is_tombstone = FALSE` tail -- a strict superset. Every row 069's index
-- covered this one covers, so removing 069's create can only widen coverage,
-- never narrow it.
--
-- Why a new NAME rather than an edited definition. This directory has no
-- applied-migration ledger: BootstrapDefinitions enumerates every file under
-- migrations/ and ApplyDefinitions Execs all of them, in filename order, on
-- EVERY bootstrap (schema.go, pinned by
-- TestApplyBootstrapExecutesDefinitionsInOrder). `CREATE INDEX ... IF NOT
-- EXISTS` matches only the index NAME, so a same-name edit is a silent no-op
-- wherever the original index already exists -- the install keeps the OLD
-- predicate forever. #5460 worked around that by pairing a DROP of the name
-- (migration 076) with a CREATE of the same name (077), which converged the
-- predicate but left every bootstrap dropping the index and rebuilding it
-- concurrently over fact_records, with no covering index between the two. A new
-- name forces the create-with-the-new-predicate exactly once on an existing
-- deployment (and once on a fresh one), then IF NOT EXISTS makes it a no-op on
-- every boot after that. Migration 105 drops the legacy name, and nothing in
-- this directory creates that name any more, so steady state issues neither an
-- index build nor a drop. 059_relationship_family_candidate_index.sql with
-- 068_drop_relationship_family_candidate_index_legacy.sql is the same shape, and
-- 101/102 is that shape for code_reachability_rows;
-- TestIdentityEpochIndexIsCreatedOnceAndNeverDropped
-- (schema_index_replay_test.go) pins the statement set, and
-- TestIdentityEpochIndexMigrationsReapplyWithoutRebuildLive proves on a
-- populated store that a second bootstrap builds nothing and drops nothing.
--
-- This create sorts BEFORE migration 105's drop of the legacy name, so an
-- install upgrading into this release builds the replacement while the legacy
-- index is still serving reads and is never left without a covering index --
-- the same ordering 059/068 and 101/102 use.
--
-- CONCURRENTLY, so building it does not block ingest writes to fact_records,
-- and the lone statement in this file because the runner Execs each file as a
-- single simple-query string and Postgres treats a multi-statement string as an
-- implicit transaction block, which CONCURRENTLY cannot run inside. The usual
-- objection to CONCURRENTLY -- that a failed build leaves an INVALID index that
-- IF NOT EXISTS then skips forever -- does not apply: the schema apply path
-- drops invalid concurrent indexes by name before executing each definition
-- (SQLDB.dropInvalidConcurrentIndexes, db.go) and runs each statement outside
-- any transaction on a dedicated bootstrap connection, which CONCURRENTLY
-- requires.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_identity_epoch_idx_v2
    ON fact_records (observed_at, fact_id)
    WHERE (
        (
            fact_kind IN ('oci_registry.image_tag_observation', 'oci_registry.image_manifest', 'oci_registry.image_index')
            AND source_system = 'oci_registry'
        )
        OR (
            fact_kind = 'aws_image_reference'
            AND source_system = 'aws'
        )
        OR (
            fact_kind = 'azure_image_reference'
            AND source_system = 'azure'
        )
        OR (
            fact_kind = 'gcp_image_reference'
            AND source_system = 'gcp'
        )
        OR (
            fact_kind = 'aws_relationship'
            AND source_system = 'aws'
            AND payload->>'target_type' = 'container_image'
        )
        OR (
            fact_kind = 'content_entity'
            AND source_system = 'git'
            AND (
                payload->'entity_metadata' ? 'container_images'
                OR payload->'metadata' ? 'container_images'
            )
        )
        OR (
            fact_kind = 'file'
            AND source_system = 'git'
            AND payload->'parsed_file_data' ? 'dockerfile_stages'
        )
    )
    AND is_tombstone = FALSE;
