-- eshu-rollback:now
-- Read-only. The replay window start: the database clock minus a clock-skew
-- margin. updated_at on work items is written by the coordinator's clock, not
-- the database's, so the margin keeps a slightly slow coordinator clock from
-- hiding replay work. A wider window only makes the replay gate stricter.
-- Variables: skew (seconds, a non-negative number).
SELECT clock_timestamp() - make_interval(secs => :'skew'::double precision);
