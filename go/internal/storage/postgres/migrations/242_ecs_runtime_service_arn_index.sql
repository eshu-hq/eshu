-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #9307: exact ECS service ARN lookup over active aws_resource facts.
-- One concurrent statement lets the migration runner build over populated
-- fact_records without blocking writers; an interrupted build is retried
-- through the runner's invalid-index cleanup.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_ecs_service_arn_idx
    ON fact_records ((payload->>'arn'))
    WHERE fact_kind = 'aws_resource'
      AND is_tombstone = FALSE
      AND payload->>'resource_type' = 'aws_ecs_service';
