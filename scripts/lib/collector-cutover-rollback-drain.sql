-- eshu-rollback:drain
-- Read-only. Work that any producer other than the in-tree instances still holds
-- for one collector kind.
-- Output: claimed_work_items|active_claims
-- Variables: kind, in_tree (comma-separated in-tree collector instance IDs).
--
-- The gate is the collector kind minus the in-tree instances, not the external
-- instance IDs the caller typed. A mistyped --external-instance therefore cannot
-- hide a claim held by the real external instance. Served by
-- workflow_work_items_claimable_idx (collector_kind, collector_instance_id,
-- status, ...).
SELECT
    (SELECT count(*)
       FROM workflow_work_items AS item
      WHERE item.collector_kind = :'kind'
        AND item.collector_instance_id <> ALL (string_to_array(:'in_tree', ','))
        AND item.status = 'claimed'),
    (SELECT count(*)
       FROM workflow_claims AS claim
       JOIN workflow_work_items AS item ON item.work_item_id = claim.work_item_id
      WHERE claim.status = 'active'
        AND item.collector_kind = :'kind'
        AND item.collector_instance_id <> ALL (string_to_array(:'in_tree', ',')));
