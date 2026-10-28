-- Index exact, active ECS service and same-generation running-task reads.
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
