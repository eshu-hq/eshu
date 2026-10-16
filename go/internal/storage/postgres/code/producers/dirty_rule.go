// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package producerstore

// ManifestLessLeg is the manifest-less dirty leg shared by PackageManifestsQuery,
// ComposerManifestsQuery, and GoModuleManifestsQuery. It returns a dirty NULL
// row for every repository scope with an active generation and no stored
// manifest of the read's kind when a generation of that scope never activated
// and could still describe content the active generation does not: per-row
// generation tags cannot see a manifest a refused generation deleted, so this
// leg is the only signal for that scope (#7609, #7760).
//
// #8243 narrows the signal. A generation with activated_at NULL marks the
// scope dirty unless it is superseded or failed and its write start
// (projection_write_started_at, NULL compared as -infinity) is at or before
// the write start of the scope's newest activated full generation (is_delta
// false, newest by ingested_at then generation_id). That full rewrote every
// content row after the generation's last write, so the generation can no
// longer hide a deleted manifest. A full without a marker (written before
// migration 149) covers nothing, because its write time is unknown; a
// pending, completed, or unknown-status generation, a generation that wrote
// after the full, and a scope with no activated full all stay dirty. This is
// the complement of the uncovered-writer definition that
// UncoveredProjectionWriters and generation retention use (#7389, #7472),
// widened to NULL markers, which post-149 code never leaves on a generation
// that wrote (the projector sets the marker before its first write).
// The LATERAL body is uncoveredProjectionWritersQuery's last_full CTE with $1
// bound to the outer scope, byte for byte; its unqualified columns resolve to
// the inner scope_generations row. TestManifestLessLegLastFullIsTheProbeLastFull
// and TestUncoveredWriterDefinitionsAgree in the parent package fail if one copy
// changes alone.
//
// The rule is more conservative than that probe: the probe skips every
// superseded or failed row with a NULL marker, while this leg keeps such a row
// dirty unless the newest activated full carries a marker.
//
// Before #8243, superseded generations that never activated kept 512 to 784
// of 812 QA repository scopes dirty per read and cost each package leg 4 to 6
// seconds of definition scan; see
// docs/internal/evidence/8243-narrow-dirty-rule.md. The newest-full lookup is
// a LEFT JOIN LATERAL, one index probe per repository scope: a correlated
// subquery per never-activated row ran 8,750 times on a QA-shaped corpus, and
// a MATERIALIZED per-scope CTE misestimated into a nested loop.
const ManifestLessLeg = `UNION ALL
SELECT scope.scope_id, NULL, 'manifest_less'
FROM ingestion_scopes AS scope
JOIN scope_generations AS active
  ON active.scope_id = scope.scope_id
 AND active.generation_id = scope.active_generation_id
 AND active.status = 'active'
LEFT JOIN LATERAL (
    SELECT projection_write_started_at AS pws
    FROM scope_generations
    WHERE scope_id = scope.scope_id
      AND is_delta = false
      AND activated_at IS NOT NULL
    ORDER BY ingested_at DESC, generation_id DESC
    LIMIT 1
) AS last_full ON true
WHERE scope.scope_kind = 'repository'
  AND NOT EXISTS (
    SELECT 1
    FROM manifest
    WHERE manifest.repo_id = scope.source_key
  )
  AND EXISTS (
    SELECT 1
    FROM scope_generations AS pending
    WHERE pending.scope_id = scope.scope_id
      AND pending.activated_at IS NULL
      AND NOT (
        pending.status IN ('superseded', 'failed')
        AND COALESCE(COALESCE(pending.projection_write_started_at, '-infinity'::timestamptz) <= last_full.pws, false)
      )
  )
`
