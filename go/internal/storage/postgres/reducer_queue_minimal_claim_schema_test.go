// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// reducerClaimCapabilityColumnsSchemaSQL keeps minimal Claim/ClaimBatch test
// schemas aligned with the migration-088 and migration-092 columns referenced
// by production SQL. These harnesses intentionally omit fact_records, so
// applying the complete container-image migrations would add unrelated schema
// dependencies.
const reducerClaimCapabilityColumnsSchemaSQL = `
ALTER TABLE fact_work_items
    ADD COLUMN IF NOT EXISTS
        container_image_identity_v2_required
        BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS
        container_image_identity_claim_epoch BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS
        container_image_identity_v2_authorized_status
        TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS
        container_image_identity_v3_required
        BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS
        container_image_identity_v3_authorized_status
        TEXT NOT NULL DEFAULT ''
`

func TestReducerClaimCapabilityColumnsSchemaTracksClaimSQL(t *testing.T) {
	t.Parallel()

	for _, column := range []string{
		"container_image_identity_v2_required",
		"container_image_identity_claim_epoch",
		"container_image_identity_v2_authorized_status",
		"container_image_identity_v3_required",
		"container_image_identity_v3_authorized_status",
	} {
		if !strings.Contains(reducerClaimCapabilityColumnsSchemaSQL, column) {
			t.Errorf("minimal Claim/ClaimBatch schema missing production column %q", column)
		}
	}
}
