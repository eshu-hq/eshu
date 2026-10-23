-- eshu-rollback:now
-- Read-only. The database clock, used as the replay window start so the
-- script never compares a workstation clock with database timestamps.
SELECT clock_timestamp();
