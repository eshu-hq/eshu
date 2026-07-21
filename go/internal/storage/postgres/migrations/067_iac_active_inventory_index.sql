-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Keep the current IaC inventory read anchored on scope/generation and its
-- narrow identity/order fields. Large payload members such as source_cache are
-- intentionally excluded so the read index stays bounded.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_iac_active_inventory_idx
    ON fact_records (
        scope_id,
        generation_id,
        (payload->>'entity_type'),
        (payload->>'entity_name'),
        (payload->>'entity_id')
    )
    WHERE fact_kind = 'content_entity'
      AND is_tombstone = FALSE
      AND payload->>'entity_type' IN (
          'TerraformResource',
          'TerraformModule',
          'TerraformDataSource'
      );
