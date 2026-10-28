// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// ecsTaskFactReadIndexesSQL anchors the read-time ECS service/task count to
// one active service ARN and one same-generation cluster/service group.
const ecsTaskFactReadIndexesSQL = `
CREATE INDEX IF NOT EXISTS fact_records_ecs_service_arn_idx
    ON fact_records ((payload->>'arn'))
    WHERE fact_kind = 'aws_resource'
      AND is_tombstone = FALSE
      AND payload->>'resource_type' = 'aws_ecs_service';

CREATE INDEX IF NOT EXISTS fact_records_ecs_task_scope_cluster_group_idx
    ON fact_records (
        scope_id, generation_id,
        ((payload->'attributes'->>'cluster_arn')),
        ((payload->'attributes'->>'group'))
    )
    WHERE fact_kind = 'aws_resource'
      AND is_tombstone = FALSE
      AND payload->>'resource_type' = 'aws_ecs_task'
      AND payload->>'state' = 'RUNNING';
`
