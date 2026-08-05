// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const ackReducerWorkQuery = `
UPDATE fact_work_items
SET status = 'succeeded', lease_owner = NULL, claim_until = NULL,
    provenance_edge_identity_upgrade_required = FALSE,
    visible_at = NULL, updated_at = $1,
    failure_class = NULL, failure_message = NULL, failure_details = NULL
WHERE work_item_id = $2
  AND stage = 'reducer'
  AND lease_owner = $3
  AND status IN ('claimed', 'running')
`

const ackContainerImageIdentityReducerWorkQuery = `
WITH acknowledged AS MATERIALIZED (
    UPDATE fact_work_items AS work
    SET status = 'succeeded',
        provenance_edge_identity_upgrade_required = FALSE,
        cross_scope_completion_ack_epoch = cross_scope_completion_ack_epoch + 1,
        container_image_identity_v2_authorized_status = CASE
            WHEN container_image_identity_v2_required THEN 'succeeded' ELSE '' END,
        container_image_identity_v3_authorized_status = CASE
            WHEN container_image_identity_v3_required THEN 'succeeded' ELSE '' END,
        lease_owner = NULL, claim_until = NULL, visible_at = NULL,
        updated_at = $1, failure_class = NULL,
        failure_message = NULL, failure_details = NULL
    WHERE work.work_item_id = $2
      AND stage = 'reducer'
      AND domain = 'container_image_identity'
      AND lease_owner = $3
      AND status IN ('claimed', 'running')
      AND container_image_identity_claim_epoch = $4
    RETURNING work.work_item_id, work.status
), emission_clock AS MATERIALIZED (
    SELECT clock_timestamp() AS emitted_at
), emitted AS (
    INSERT INTO cross_scope_completion_events (
        producer_domain, producer_item_count, status,
        visible_at, created_at, updated_at
    )
    SELECT 'container_image_identity', count(*)::BIGINT, 'pending',
           emission_clock.emitted_at + INTERVAL '250 milliseconds',
           emission_clock.emitted_at, emission_clock.emitted_at
    FROM acknowledged CROSS JOIN emission_clock
    WHERE acknowledged.status = 'succeeded'
    GROUP BY emission_clock.emitted_at
    ON CONFLICT (producer_domain) WHERE status IN ('pending', 'retrying') DO UPDATE SET
        producer_item_count = cross_scope_completion_events.producer_item_count + EXCLUDED.producer_item_count,
        visible_at = CASE
            WHEN cross_scope_completion_events.status = 'retrying'
                THEN cross_scope_completion_events.visible_at
            ELSE LEAST(cross_scope_completion_events.created_at + INTERVAL '2 seconds', EXCLUDED.visible_at)
        END,
        updated_at = EXCLUDED.updated_at
    RETURNING 1
)
SELECT 1 FROM acknowledged LIMIT 1
`

const ackCICDRunCorrelationReducerWorkQuery = `
WITH acknowledged AS MATERIALIZED (
    UPDATE fact_work_items AS work
    SET status = 'succeeded',
        cross_scope_completion_ack_epoch = cross_scope_completion_ack_epoch + 1,
        lease_owner = NULL, claim_until = NULL, visible_at = NULL,
        updated_at = $1, failure_class = NULL,
        failure_message = NULL, failure_details = NULL
    WHERE work.work_item_id = $2
      AND stage = 'reducer'
      AND domain = 'ci_cd_run_correlation'
      AND lease_owner = $3
      AND status IN ('claimed', 'running')
    RETURNING work.work_item_id, work.status
), emission_clock AS MATERIALIZED (
    SELECT clock_timestamp() AS emitted_at
), emitted AS (
    INSERT INTO cross_scope_completion_events (
        producer_domain, producer_item_count, status,
        visible_at, created_at, updated_at
    )
    SELECT 'ci_cd_run_correlation', count(*)::BIGINT, 'pending',
           emission_clock.emitted_at + INTERVAL '250 milliseconds',
           emission_clock.emitted_at, emission_clock.emitted_at
    FROM acknowledged CROSS JOIN emission_clock
    WHERE acknowledged.status = 'succeeded'
    GROUP BY emission_clock.emitted_at
    ON CONFLICT (producer_domain) WHERE status IN ('pending', 'retrying') DO UPDATE SET
        producer_item_count = cross_scope_completion_events.producer_item_count + EXCLUDED.producer_item_count,
        visible_at = CASE
            WHEN cross_scope_completion_events.status = 'retrying'
                THEN cross_scope_completion_events.visible_at
            ELSE LEAST(cross_scope_completion_events.created_at + INTERVAL '2 seconds', EXCLUDED.visible_at)
        END,
        updated_at = EXCLUDED.updated_at
    RETURNING 1
)
SELECT 1 FROM acknowledged LIMIT 1
`
