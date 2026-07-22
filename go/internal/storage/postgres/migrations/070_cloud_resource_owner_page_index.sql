-- #5563: page CloudResource identities from the deterministic owner ledger.
-- This partial index preserves the resource_type, uid keyset order without
-- copying the full winning_row JSONB value into the index.
CREATE INDEX CONCURRENTLY IF NOT EXISTS graph_node_owner_cloud_resource_page_idx
    ON graph_node_owner (((winning_row->>'resource_type')), uid)
    WHERE winning_row->>'resource_type' IS NOT NULL;
