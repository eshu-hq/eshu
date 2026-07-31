// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// reducerClaimCapabilityColumnsSchemaSQL keeps minimal Claim/ClaimBatch test
// schemas aligned with the migration-088 columns referenced by production SQL.
// These harnesses intentionally omit fact_records, so applying the complete
// container-image cutover migration would add unrelated schema dependencies.
const reducerClaimCapabilityColumnsSchemaSQL = `
ALTER TABLE fact_work_items
    ADD COLUMN IF NOT EXISTS
        container_image_identity_v2_required
        BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS
        container_image_identity_claim_epoch BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS
        container_image_identity_v2_authorized_status
        TEXT NOT NULL DEFAULT ''
`
