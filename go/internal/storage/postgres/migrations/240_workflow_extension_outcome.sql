-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- A completed SDK extension claim records its bounded outcome on the fenced
-- workflow item. Legacy and in-tree rows remain NULL (unknown).
ALTER TABLE workflow_work_items
    ADD COLUMN IF NOT EXISTS extension_result_state TEXT,
    ADD COLUMN IF NOT EXISTS extension_partial BOOLEAN,
    ADD COLUMN IF NOT EXISTS extension_warning_count INTEGER;
