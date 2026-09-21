// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// listCodeDriftedPairsQuery nominates drifted-pair candidates for one repo
// from the LSH band side table (epic #6833, child #6837). $1 is the repo_id,
// $2 the token floor, $3 the per-entity candidate budget.
//
// Shape: the band self-join is served by the (repo_id, band_no, band_hash)
// lookup index from migration 111 — the #6834 EXPLAIN evidence (single-repo
// band join 4.0ms indexed) requires that predicate shape, so keep the
// equi-join on all three columns. Pairs sharing an equality fingerprint
// (fp_exact or fp_renamed) are excluded before ranking: the #6836 read
// surface owns them, and counting them against the budget would drop genuine
// drift below the cut. Ranking is per-entity by shared-band count desc with
// ROW_NUMBER window counts; a pair verifies when EITHER endpoint ranks it
// within budget (union semantics preserve the measured 100% ship-band
// recall at K = 200). The c1/c2 partner totals ride along so the loader can
// flag budget-exhausted entities for telemetry without a second pass.
const listCodeDriftedPairsQuery = `
WITH pairs AS (
    SELECT a.entity_id AS e1, b.entity_id AS e2, COUNT(*) AS shared
    FROM code_fingerprint_band AS a
    JOIN code_fingerprint_band AS b
      ON b.repo_id = a.repo_id
     AND b.band_no = a.band_no
     AND b.band_hash = a.band_hash
     AND b.entity_id > a.entity_id
    WHERE a.repo_id = $1
    GROUP BY a.entity_id, b.entity_id
),
nominated AS (
    SELECT p.e1, p.e2, p.shared
    FROM pairs AS p
    JOIN code_function_fingerprint AS fa ON fa.entity_id = p.e1
    JOIN code_function_fingerprint AS fb ON fb.entity_id = p.e2
    WHERE fa.token_count >= $2 AND fb.token_count >= $2
      AND fa.shingles IS NOT NULL AND fb.shingles IS NOT NULL
      AND fa.fp_exact <> fb.fp_exact
      AND fa.fp_renamed <> fb.fp_renamed
),
ranked AS (
    SELECT n.e1, n.e2, n.shared,
        ROW_NUMBER() OVER (PARTITION BY n.e1 ORDER BY n.shared DESC, n.e2 ASC) AS r1,
        ROW_NUMBER() OVER (PARTITION BY n.e2 ORDER BY n.shared DESC, n.e1 ASC) AS r2,
        COUNT(*) OVER (PARTITION BY n.e1) AS c1,
        COUNT(*) OVER (PARTITION BY n.e2) AS c2
    FROM nominated AS n
)
SELECT e1, e2, shared, c1, c2
FROM ranked
WHERE r1 <= $3 OR r2 <= $3
ORDER BY shared DESC, e1 ASC, e2 ASC
`

// listCodeDriftedMembersQuery loads the full member rows for the pair
// entity IDs the pairs query kept: fingerprint columns plus the
// content_entities identity both the suppression rules and the finding
// payload read. $1 is the repo_id, $2 the entity ID set. It reads narrow
// fingerprint columns and entity rows only — never source_cache. The
// shingles predicate matches the pairs gate: a nominated entity whose
// fingerprint row lost its shingle set between the two reads (exact-only
// tiers NULL shingles and fp_renamed together) simply does not return, so
// the pair drops with a no_shingles count instead of failing the scan and
// the whole load.
const listCodeDriftedMembersQuery = `
SELECT f.entity_id, f.fp_exact, f.fp_renamed, f.shingles, f.token_count,
       e.entity_name, e.entity_type, e.relative_path, coalesce(e.language, ''),
       e.start_line, e.end_line
FROM code_function_fingerprint AS f
JOIN content_entities AS e ON e.entity_id = f.entity_id AND e.repo_id = f.repo_id
WHERE f.repo_id = $1 AND f.entity_id = ANY($2::text[]) AND f.shingles IS NOT NULL
ORDER BY f.entity_id ASC
`

// countCodeDriftedExclusionsQuery counts fingerprinted rows that can never
// become candidate pairs, so the generation suppression totals stay
// complete: below-floor rows, rows with no persisted shingle set (pre-#6837
// payloads, plus rows whose renamed hash went missing), all for one repo.
// Equality-duplicate pairs are pair-shaped, not row-shaped, so they are
// counted by countCodeDriftedEqualityDuplicatesQuery instead.
const countCodeDriftedExclusionsQuery = `
SELECT
    COUNT(*) FILTER (WHERE token_count < $2) AS below_floor,
    COUNT(*) FILTER (WHERE token_count >= $2 AND (shingles IS NULL OR fp_renamed IS NULL)) AS no_shingles
FROM code_function_fingerprint
WHERE repo_id = $1
`

// countCodeDriftedEqualityDuplicatesQuery counts band pairs the ownership
// exclusion removes: pairs that pass the floor and shingle gates but share
// an equality fingerprint, so the #6836 surface owns them. $1 is the
// repo_id, $2 the token floor. It mirrors the pairs CTE of
// listCodeDriftedPairsQuery without the ranking: same nomination, opposite
// filter.
const countCodeDriftedEqualityDuplicatesQuery = `
WITH pairs AS (
    SELECT a.entity_id AS e1, b.entity_id AS e2
    FROM code_fingerprint_band AS a
    JOIN code_fingerprint_band AS b
      ON b.repo_id = a.repo_id
     AND b.band_no = a.band_no
     AND b.band_hash = a.band_hash
     AND b.entity_id > a.entity_id
    WHERE a.repo_id = $1
    GROUP BY a.entity_id, b.entity_id
)
SELECT COUNT(*) AS equality_duplicates
FROM pairs AS p
JOIN code_function_fingerprint AS fa ON fa.entity_id = p.e1
JOIN code_function_fingerprint AS fb ON fb.entity_id = p.e2
WHERE fa.token_count >= $2 AND fb.token_count >= $2
  AND fa.shingles IS NOT NULL AND fb.shingles IS NOT NULL
  AND (fa.fp_exact = fb.fp_exact OR fa.fp_renamed = fb.fp_renamed)
`
