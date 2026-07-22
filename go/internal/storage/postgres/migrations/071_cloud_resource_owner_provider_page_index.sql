-- #5563: keep provider-filtered CloudResource pages index ordered.
CREATE INDEX CONCURRENTLY IF NOT EXISTS graph_node_owner_cloud_resource_provider_page_idx
    ON graph_node_owner (((winning_row->>'collector_kind')), ((winning_row->>'resource_type')), uid)
    WHERE winning_row->>'resource_type' IS NOT NULL;
