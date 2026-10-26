// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package actionsstore

const schemaSQL = `
CREATE TABLE IF NOT EXISTS cicd_actions_deliveries (
  delivery_id text PRIMARY KEY,
  event_kind text NOT NULL,
  action text NOT NULL,
  repository_id text NOT NULL,
  repository text NOT NULL,
  run_id text NOT NULL,
  job_id text NOT NULL DEFAULT '',
  handoff_key text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'queued',
  claim_fencing_token bigint NOT NULL DEFAULT 0,
  claim_attempts integer NOT NULL DEFAULT 0,
  claim_expires_at timestamptz,
  visible_at timestamptz NOT NULL,
  received_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  failure_class text NOT NULL DEFAULT '',
  CONSTRAINT cicd_actions_deliveries_status_check CHECK (status IN ('queued','claimed','handed_off','failed'))
);
CREATE INDEX IF NOT EXISTS cicd_actions_deliveries_queued_idx
  ON cicd_actions_deliveries (visible_at,received_at,delivery_id) WHERE status='queued';
CREATE INDEX IF NOT EXISTS cicd_actions_deliveries_pending_repo_idx
  ON cicd_actions_deliveries (repository) WHERE status IN ('queued','claimed');
CREATE INDEX IF NOT EXISTS cicd_actions_deliveries_expired_idx
  ON cicd_actions_deliveries (claim_expires_at,delivery_id) WHERE status='claimed';
`

// SchemaSQL returns the Actions delivery inbox DDL for bootstrap parity tests.
func SchemaSQL() string { return schemaSQL }
