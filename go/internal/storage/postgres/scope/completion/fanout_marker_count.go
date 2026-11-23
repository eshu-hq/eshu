// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package completionstore

import "time"

// activeGenerationsCTE names the scope-to-active-generation set. The fan-out
// statement and the skipped-marker count both splice this one text, so the
// count can never read a different generation set than the one the fan-out
// scheduled from.
const activeGenerationsCTE = `active_generations AS MATERIALIZED (
    SELECT scope.scope_id, generation.generation_id
    FROM ingestion_scopes AS scope
    JOIN scope_generations AS generation
      ON generation.scope_id = scope.scope_id
     AND generation.generation_id = scope.active_generation_id
     AND generation.status = 'active'
)`

// activeGenerationJoin restricts a fact_work_items alias named source to the
// active generation of its scope. Shared by the fan-out candidate set and the
// skipped-marker count.
const activeGenerationJoin = `JOIN active_generations AS generation
      ON generation.scope_id = source.scope_id
     AND generation.generation_id = source.generation_id`

// consumerStatePredicate is the stage and status filter for a consumer work
// item a completion event can affect. Shared by the fan-out candidate set and
// the skipped-marker count.
const consumerStatePredicate = `source.stage = 'reducer'
      AND source.status IN ('claimed', 'running', 'succeeded')`

// manifestMarkerCountQuery counts the manifest markers the fan-out left alone
// (#10972): supply_chain_impact items at an active generation that match
// manifestMarkerPredicate. It is a plain read: no FOR clause, no DML, no
// parameters. Fanout runs it as its own autocommit statement after the fan-out
// statement has committed, so it holds no lock the fan-out or a producer ack
// waits on. The markers are not scheduled, so the count feeds only
// eshu_dp_cross_scope_completion_markers_skipped_total.
const manifestMarkerCountQuery = `
WITH ` + activeGenerationsCTE + `
SELECT count(*)
FROM fact_work_items AS source
` + activeGenerationJoin + `
WHERE ` + consumerStatePredicate + `
  AND (` + manifestMarkerPredicate + `)
`

// defaultManifestMarkerCountTimeout bounds the post-commit marker count. The
// count measured 1,082 ms on the QA replica, so ten seconds is about ten times
// the measured cost: long enough that a loaded database still reports, short
// enough that the runner's per-event Fanout call is not held for a counter.
const defaultManifestMarkerCountTimeout = 10 * time.Second
