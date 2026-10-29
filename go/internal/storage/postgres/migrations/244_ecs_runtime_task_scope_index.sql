-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #9307: same-generation running ECS tasks by exact cluster and service group.
-- This is a separate concurrent statement because PostgreSQL rejects a
-- concurrent index build inside the implicit transaction of a multi-statement
-- migration message.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_ecs_task_scope_cluster_group_idx
    ON fact_records (
        scope_id, generation_id,
        ((payload->'attributes'->>'cluster_arn')),
        ((payload->'attributes'->>'group'))
    )
    WHERE fact_kind = 'aws_resource'
      AND is_tombstone = FALSE
      AND payload->>'resource_type' = 'aws_ecs_task'
      AND payload->>'state' = 'RUNNING';
