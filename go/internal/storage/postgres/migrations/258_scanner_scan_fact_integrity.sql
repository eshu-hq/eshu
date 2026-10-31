-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Generic fact rows remain mutable projections. A derived scanner reader must
-- compare each member with the protected payload and claim fence before use.
-- There is no historical backfill: old associations without these columns
-- cannot be promoted to current source authority.
ALTER TABLE scanner_image_scan_fact_associations
    ADD COLUMN IF NOT EXISTS payload_sha256 TEXT NOT NULL
      CHECK (payload_sha256 ~ '^[0-9a-f]{64}$');
ALTER TABLE scanner_image_scan_fact_associations
    ADD COLUMN IF NOT EXISTS fencing_token BIGINT NOT NULL
      CHECK (fencing_token > 0);
