-- #10601 (reopened) leg1 summary read: the snapshot read no longer probes
-- workflow_work_items per active scope; the resweep maintains the
-- per-scope collector-instance identity on this column instead
-- (rebuildCollectorEvidenceSummarySQL). Existing rows keep '' until the next
-- resweep reconciles them (startup backfill plus the 60s cadence), so the
-- read stays shape-identical throughout; values converge within one cadence.
ALTER TABLE collector_evidence_summary
    ADD COLUMN IF NOT EXISTS collector_instance_id TEXT NOT NULL DEFAULT '';
