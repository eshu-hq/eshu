-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Build separately from the column migration. A plain index build on the
-- workflow queue would block writers during deployment.
CREATE INDEX CONCURRENTLY IF NOT EXISTS workflow_extension_outcome_latest_idx
    ON workflow_work_items (collector_kind, collector_instance_id, scope_id, updated_at DESC, work_item_id DESC)
    INCLUDE (extension_partial, extension_warning_count)
    WHERE status = 'completed' AND extension_result_state IS NOT NULL;
