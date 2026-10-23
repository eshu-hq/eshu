-- eshu-rollback:drain
-- Read-only. Work the external producer still holds for one collector kind.
-- Output: claimed_work_items|active_claims
-- Variables: kind, instances (comma-separated collector instance IDs).
SELECT
    (SELECT count(*)
       FROM workflow_work_items AS item
      WHERE item.collector_kind = :'kind'
        AND item.collector_instance_id = ANY (string_to_array(:'instances', ','))
        AND item.status = 'claimed'),
    (SELECT count(*)
       FROM workflow_claims AS claim
       JOIN workflow_work_items AS item ON item.work_item_id = claim.work_item_id
      WHERE claim.status = 'active'
        AND item.collector_kind = :'kind'
        AND item.collector_instance_id = ANY (string_to_array(:'instances', ',')));
