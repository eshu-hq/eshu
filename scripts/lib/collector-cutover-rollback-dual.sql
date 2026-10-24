-- eshu-rollback:dual
-- Read-only. Detects two producers holding authority for one source scope.
-- Output: external_claimed|in_tree_claimed|overlapping_claim_pairs
-- Variables: kind, in_tree (comma-separated in-tree collector instance IDs).
--
-- "External" means every instance of the collector kind that is not an in-tree
-- instance, so a mistyped --external-instance cannot hide the real one.
--
-- A claim holds authority from claimed_at until it ends. A finished claim ends
-- at the earlier of finished_at and lease_expires_at, so a late reap does not
-- invent an overlap; an unfinished claim has not ended. Two claims on the same
-- scope_id by an external and an in-tree instance whose spans intersect are
-- dual authority.
SELECT
    (SELECT count(*)
       FROM workflow_work_items
      WHERE collector_kind = :'kind'
        AND status = 'claimed'
        AND collector_instance_id <> ALL (string_to_array(:'in_tree', ','))),
    (SELECT count(*)
       FROM workflow_work_items
      WHERE collector_kind = :'kind'
        AND status = 'claimed'
        AND collector_instance_id = ANY (string_to_array(:'in_tree', ','))),
    (SELECT count(*)
       FROM workflow_claims AS ext_claim
       JOIN workflow_work_items AS ext_item ON ext_item.work_item_id = ext_claim.work_item_id
       JOIN workflow_work_items AS tree_item
         ON tree_item.collector_kind = ext_item.collector_kind
        AND tree_item.scope_id = ext_item.scope_id
       JOIN workflow_claims AS tree_claim ON tree_claim.work_item_id = tree_item.work_item_id
      WHERE ext_item.collector_kind = :'kind'
        AND ext_item.collector_instance_id <> ALL (string_to_array(:'in_tree', ','))
        AND tree_item.collector_instance_id = ANY (string_to_array(:'in_tree', ','))
        AND ext_claim.claimed_at < CASE WHEN tree_claim.finished_at IS NULL THEN 'infinity'::timestamptz
                                        ELSE LEAST(tree_claim.finished_at, tree_claim.lease_expires_at) END
        AND tree_claim.claimed_at < CASE WHEN ext_claim.finished_at IS NULL THEN 'infinity'::timestamptz
                                         ELSE LEAST(ext_claim.finished_at, ext_claim.lease_expires_at) END);
