-- #5469: resolve current runtime image evidence from the owner ledger without
-- scanning the CloudResource graph label. The key order also satisfies the
-- deterministic digest/ARN/uid result order before the bounded LIMIT.
CREATE INDEX CONCURRENTLY IF NOT EXISTS graph_node_owner_cloud_resource_runtime_digest_idx
    ON graph_node_owner (((winning_row->>'running_image_digest')), ((winning_row->>'arn')), uid)
    WHERE winning_row->>'resource_type' IS NOT NULL
      AND NULLIF(BTRIM(winning_row->>'running_image_digest'), '') IS NOT NULL
      AND NULLIF(BTRIM(winning_row->>'arn'), '') IS NOT NULL;
