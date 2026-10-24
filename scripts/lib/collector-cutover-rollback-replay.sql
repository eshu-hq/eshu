-- eshu-rollback:replay
-- Read-only. Replay progress of the restored in-tree producer.
-- Output: pending|claimed|completed|retryable|terminal
-- Variables: kind, instances (in-tree), since (database timestamp).
-- retryable counts failed_retryable and expired, both non-terminal.
SELECT
    count(*) FILTER (WHERE status = 'pending'),
    count(*) FILTER (WHERE status = 'claimed'),
    count(*) FILTER (WHERE status = 'completed'),
    count(*) FILTER (WHERE status IN ('failed_retryable', 'expired')),
    count(*) FILTER (WHERE status = 'failed_terminal')
FROM workflow_work_items
WHERE collector_kind = :'kind'
  AND collector_instance_id = ANY (string_to_array(:'instances', ','))
  AND updated_at >= :'since'::timestamptz;
