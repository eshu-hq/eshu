// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// insertServiceMaterializationGenerationQuery inserts one new active service
// generation. The id is deterministic in the evidence set, so ON CONFLICT
// (generation_id) DO NOTHING makes an identical re-materialization a no-op: zero
// rows affected signals the caller to skip supersession and snapshot writes.
//
// Parameter order:
//
//	$1 generation_id
//	$2 service_id
//	$3 trigger_kind
//	$4 source_intent_id (nullable)
//	$5 observed_at
//	$6 ingested_at
//	$7 status
//	$8 activated_at
//	$9 payload (jsonb)
const insertServiceMaterializationGenerationQuery = `
INSERT INTO service_materialization_generations (
    generation_id,
    service_id,
    trigger_kind,
    source_intent_id,
    observed_at,
    ingested_at,
    status,
    activated_at,
    payload
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb
)
ON CONFLICT (generation_id) DO NOTHING
`

// supersedePriorServiceGenerationQuery retires the prior active generation for a
// service when a new active generation is committed. It excludes the new
// generation so the freshly inserted active row is never superseded, and returns
// the superseded generation id (or no row when the service had no prior active
// generation). The partial unique index keeps at most one active row per
// service, so this updates at most one row.
//
// Parameter order:
//
//	$1 service_id
//	$2 new_generation_id (excluded from supersession)
//	$3 superseded_at
const supersedePriorServiceGenerationQuery = `
UPDATE service_materialization_generations
SET status = 'superseded',
    superseded_at = $3
WHERE service_id = $1
  AND status = 'active'
  AND generation_id <> $2
RETURNING generation_id
`

// activateServiceGenerationQuery promotes the freshly inserted pending
// generation to active. It runs after the prior active generation is superseded,
// so the single-active-per-service partial unique index is satisfied. It scopes
// by both service_id and generation_id so it only ever activates the generation
// this commit just inserted.
//
// Parameter order:
//
//	$1 service_id
//	$2 generation_id
//	$3 activated_at
const activateServiceGenerationQuery = `
UPDATE service_materialization_generations
SET status = 'active',
    activated_at = $3
WHERE service_id = $1
  AND generation_id = $2
  AND status = 'pending'
`

// insertServiceEvidenceSnapshotQuery writes one generation-stable evidence row.
// The identity is (generation_id, service_evidence_key); the generation lives in
// the row, never in the key. ON CONFLICT keeps the write idempotent so a retried
// commit converges on the same row instead of duplicating it.
//
// Parameter order:
//
//	$1 generation_id
//	$2 service_id
//	$3 evidence_family
//	$4 service_evidence_key
//	$5 payload_hash
//	$6 is_tombstone
//	$7 observed_at
//	$8 payload (jsonb)
const insertServiceEvidenceSnapshotQuery = `
INSERT INTO service_evidence_snapshots (
    generation_id,
    service_id,
    evidence_family,
    service_evidence_key,
    payload_hash,
    is_tombstone,
    observed_at,
    payload
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8::jsonb
)
ON CONFLICT (generation_id, service_evidence_key) DO UPDATE SET
    service_id = EXCLUDED.service_id,
    evidence_family = EXCLUDED.evidence_family,
    payload_hash = EXCLUDED.payload_hash,
    is_tombstone = EXCLUDED.is_tombstone,
    observed_at = EXCLUDED.observed_at,
    payload = EXCLUDED.payload
`
