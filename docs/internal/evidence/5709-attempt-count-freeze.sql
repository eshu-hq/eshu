-- #5709 attempt_count-freeze theory-proof: enrolling
-- cross_scope_producer_not_ready in nonCountingReducerRetryFailureClasses must
-- make a RETRYING row in that class keep its attempt_count on claim, while a
-- counting class still increments and a non-retrying row still increments. This
-- mirrors reducerClaimAttemptCountCaseSQL()'s exact assignment (aliased "work").

CREATE TABLE fact_work_items (
    work_item_id TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    failure_class TEXT NULL,
    attempt_count INT NOT NULL
);

INSERT INTO fact_work_items (work_item_id, status, failure_class, attempt_count) VALUES
    ('retrying-crossscope',  'retrying', 'cross_scope_producer_not_ready', 2),  -- expect FROZEN at 2
    ('retrying-counting',    'retrying', 'graph_write_timeout',            2),  -- expect INCREMENTED to 3
    ('running-crossscope',   'running',  'cross_scope_producer_not_ready', 2);  -- expect INCREMENTED to 3 (status guard)

-- The exact assignment reducerClaimAttemptCountCaseSQL() renders, with the new
-- class present in the exempt disjunction.
UPDATE fact_work_items AS work
SET attempt_count = CASE
        WHEN work.status = 'retrying' AND (
                 work.failure_class = 'secrets_iam_endpoint_not_ready'
              OR work.failure_class = 'kubernetes_correlation_nodes_not_ready'
              OR work.failure_class = 'gcp_relationship_nodes_not_ready'
              OR work.failure_class = 'ec2_instance_identity_nodes_not_ready'
              OR work.failure_class = 'cross_scope_producer_not_ready'
             ) THEN work.attempt_count
        ELSE work.attempt_count + 1
    END;

SELECT work_item_id, status, failure_class, attempt_count,
       CASE work_item_id
           WHEN 'retrying-crossscope' THEN (attempt_count = 2)
           WHEN 'retrying-counting'   THEN (attempt_count = 3)
           WHEN 'running-crossscope'  THEN (attempt_count = 3)
       END AS matches_expected
FROM fact_work_items
ORDER BY work_item_id;
