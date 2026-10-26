// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package actionsstore

const selectColumns = `delivery_id,event_kind,action,repository_id,repository,run_id,job_id,handoff_key,status,claim_fencing_token,claim_attempts,claim_expires_at,received_at,updated_at`

const insertSQL = `INSERT INTO cicd_actions_deliveries
 (delivery_id,event_kind,action,repository_id,repository,run_id,job_id,visible_at,received_at,updated_at)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8,$8) ON CONFLICT (delivery_id) DO NOTHING`

const claimSQL = `WITH next AS MATERIALIZED (
 SELECT delivery_id,repository,repository_id,handoff_key FROM cicd_actions_deliveries
 WHERE status='queued' AND visible_at<=$3 ORDER BY visible_at ASC,received_at ASC,delivery_id ASC LIMIT $1 FOR UPDATE SKIP LOCKED
), grouped AS MATERIALIZED (
 SELECT delivery_id,
 min(delivery_id) FILTER (WHERE handoff_key='') OVER (PARTITION BY repository,repository_id) AS anchor
 FROM next
) UPDATE cicd_actions_deliveries AS delivery
 SET status='claimed',claim_fencing_token=delivery.claim_fencing_token+1,
 claim_attempts=delivery.claim_attempts+1,claim_expires_at=$2,updated_at=$3,
 handoff_key=CASE WHEN delivery.handoff_key='' THEN 'batch-v1:'||grouped.anchor ELSE delivery.handoff_key END
 FROM grouped WHERE delivery.delivery_id=grouped.delivery_id
 RETURNING delivery.delivery_id,delivery.event_kind,delivery.action,delivery.repository_id,
 delivery.repository,delivery.run_id,delivery.job_id,delivery.handoff_key,delivery.status,delivery.claim_fencing_token,
 delivery.claim_attempts,delivery.claim_expires_at,delivery.received_at,delivery.updated_at`

const reapSQL = `WITH expired AS (
 SELECT delivery_id FROM cicd_actions_deliveries WHERE status='claimed'
 AND claim_expires_at<$1 ORDER BY claim_expires_at ASC,delivery_id ASC
 LIMIT $2 FOR UPDATE SKIP LOCKED
) UPDATE cicd_actions_deliveries AS delivery
 SET status=CASE WHEN claim_attempts >= $3 THEN 'failed' ELSE 'queued' END,
 failure_class=CASE WHEN claim_attempts >= $3 THEN 'attempts_exhausted' ELSE '' END,
 claim_expires_at=NULL,visible_at=$1,updated_at=$1
 FROM expired WHERE delivery.delivery_id=expired.delivery_id
 RETURNING delivery.status`

const ackSQL = `UPDATE cicd_actions_deliveries SET status='handed_off',failure_class='',claim_expires_at=NULL,updated_at=$3
 WHERE delivery_id=$1 AND claim_fencing_token=$2 AND status='claimed' AND claim_expires_at>$3`

const releaseSQL = `UPDATE cicd_actions_deliveries
 SET status=CASE WHEN claim_attempts >= $4 AND $5 NOT IN ('blocked','no_active_instance') THEN 'failed' ELSE 'queued' END,
 failure_class=CASE WHEN claim_attempts >= $4 AND $5 NOT IN ('blocked','no_active_instance') THEN 'attempts_exhausted' ELSE $5 END,
 claim_attempts=CASE WHEN $5 IN ('blocked','no_active_instance') THEN greatest(claim_attempts-1,0) ELSE claim_attempts END,
 claim_expires_at=NULL,visible_at=CASE WHEN $5 IN ('blocked','no_active_instance') THEN $3+interval '30 seconds' ELSE $3 END,updated_at=$3
 WHERE delivery_id=$1 AND claim_fencing_token=$2 AND status='claimed' AND claim_expires_at>$3`

const failSQL = `UPDATE cicd_actions_deliveries SET status='failed',failure_class=$4,
 claim_expires_at=NULL,updated_at=$3
 WHERE delivery_id=$1 AND claim_fencing_token=$2 AND status='claimed' AND claim_expires_at>$3`
