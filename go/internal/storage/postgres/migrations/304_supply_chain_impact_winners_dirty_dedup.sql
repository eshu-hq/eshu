-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10827 (R2: Global Key-Ordered Versioned Upsert): collapse the backlog the
-- append-only era piled up, keeping the earliest mark per key: min(seq) stays
-- queued with its first-mark captured_at, and the duplicates leave. DML only:
-- the dirty_version column rides 303, and table-locking DDL and bulk DML
-- must not share a file (#7961). The DELETEs take only row-level locks and
-- are one-time: the 305/306 unique indexes forbid new duplicates from
-- forming, and the drain tally sums versions from here on.
--
-- Rollback: re-appending collapsed rows is not possible from this file
-- alone; the resweep re-marks live keys on the next tick, so a rollback
-- loses nothing but the collapsed history.

-- Collapse duplicate fact-kind marks, keeping the earliest mark per key.
DELETE FROM supply_chain_impact_winners_dirty_log AS d
USING (
    SELECT seq
    FROM (
        SELECT seq, row_number() OVER (
            PARTITION BY package_id, repository_id, subject_digest, c0, advisory_id
            ORDER BY seq
        ) AS rn
        FROM supply_chain_impact_winners_dirty_log
        WHERE kind = 'fact'
    ) AS ranked
    WHERE rn > 1
) AS dup
WHERE d.seq = dup.seq;

-- Collapse duplicate scope-kind marks, keeping the earliest mark per pair.
DELETE FROM supply_chain_impact_winners_dirty_log AS d
USING (
    SELECT seq
    FROM (
        SELECT seq, row_number() OVER (
            PARTITION BY scope_id, generation_id
            ORDER BY seq
        ) AS rn
        FROM supply_chain_impact_winners_dirty_log
        WHERE kind = 'scope'
    ) AS ranked
    WHERE rn > 1
) AS dup
WHERE d.seq = dup.seq;
