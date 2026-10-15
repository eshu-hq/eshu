-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7666: ARN probe for the aws_cloud_runtime_drift state join.
-- listActiveStateResourcesForAWSARNsQuery
-- (aws_cloud_runtime_drift_evidence_sql.go) matches Terraform state to one
-- AWS generation on payload->'attributes'->>'arn'. No index carried that
-- expression, so the planner ran the active-state join once per AWS ARN:
-- 129,618 index scans of fact_records_scope_generation_idx and 11.16M buffers
-- (36 s) for 379 ARNs x 342 state scopes on the 1/20-scale shim, and 14-20
-- minutes per drift run in production.
--
-- The key and predicate match the statement's state-side probe exactly
-- (fact_kind = 'terraform_state_resource' plus the non-blank ARN guard), so
-- each ARN is one index descent. The fenced statement probes this index once
-- per ARN: 1,625-2,668 buffers and about 4 ms on the same shim, with fresh,
-- absent, or stale table statistics. No expression statistics object goes
-- with it: the fenced statement does not need one, and a stale one left the
-- unchanged statement 2,358x slower than a fresh one. Evidence:
-- docs/internal/evidence/7666-aws-drift-arn-index.md.
--
-- Cost: 6.0 MB for 276,075 entries on the shim (one entry per state resource
-- row that carries an ARN, every retained generation). Each inserted row that
-- carries an ARN adds one index WAL record: +12.5k records (+4.9%) per 50,000
-- inserted terraform_state_resource rows at 25% ARN-bearing, per-statement
-- pg_stat_statements on PostgreSQL 18.6. The
-- concurrent build took 1.0 s over 1.68M rows (938 MB heap) on an idle host;
-- plan a minute-scale build on the production primary. Watch it with
-- SELECT phase, blocks_done, blocks_total FROM pg_stat_progress_create_index.
--
-- This file must remain one concurrent statement: PostgreSQL rejects CREATE
-- INDEX CONCURRENTLY in a transaction block, and the migration coordinator
-- executes sole concurrent-index definitions in autocommit mode. The schema
-- executor clears an invalid same-name index before retrying a failed build.
-- The predicate MUST stay identical to the statement's LATERAL probe;
-- TestFactRecordsTerraformStateResourceARNIndexMatchesDriftProbe locks the two
-- together.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_terraform_state_resource_arn_idx
    ON fact_records ((payload->'attributes'->>'arn'))
    WHERE fact_kind = 'terraform_state_resource'
      AND btrim(COALESCE(payload->'attributes'->>'arn', '')) <> '';
