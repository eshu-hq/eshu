-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- The in-flight arm's hold: a disposable-replay-database trigger that holds the projector's write
-- marker update of a real fixture scope while replay_hold.hold is true. Installed by
-- lib/inflight.sh (replay_inflight_install_hold); a file rather than a heredoc for the heredoc budget gate.
CREATE TABLE replay_hold (hold boolean NOT NULL);
INSERT INTO replay_hold VALUES (true);
CREATE FUNCTION replay_hold_marker() RETURNS trigger LANGUAGE plpgsql AS $fn$
BEGIN
  WHILE (SELECT hold FROM replay_hold) LOOP
    PERFORM pg_sleep(0.05);
  END LOOP;
  RETURN NEW;
END
$fn$;
CREATE TRIGGER replay_hold_marker BEFORE UPDATE OF projection_write_started_at ON scope_generations
  FOR EACH ROW WHEN (NEW.projection_write_started_at IS NOT NULL AND NEW.scope_id LIKE 'git-repository-scope:%')
  EXECUTE FUNCTION replay_hold_marker();
