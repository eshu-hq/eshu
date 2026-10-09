-- usage: psql -v n=<rows>; rebuilds repository_retirements with n rows (90% open, 10% readmitted history)
TRUNCATE repository_retirements;
INSERT INTO repository_retirements (retirement_id, repo_id, scope_id, state, phase, reason_code, reason_hash, actor_class, idempotency_key_hash, scope_id_hash, generation_ids_hash, generations_fenced, next_attempt_at, requested_at, updated_at, retired_at, readmitted_at)
SELECT 'rr_'||lpad(i::text,14,'0'), 'repo:rr:'||lpad(i::text,6,'0'), 'scope:rr:'||i,
       (ARRAY['complete','complete','pending','running','blocked','failed'])[1 + i % 6], 'done', 'operator_retired', 'h','admin','k','s','g', 3,
       now(), now(), now(), now(), CASE WHEN i % 10 = 0 THEN now() END
FROM generate_series(1, :n) i;
