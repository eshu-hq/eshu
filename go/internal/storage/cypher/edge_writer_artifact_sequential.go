// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"time"
)

// executeArtifactStatements runs repo_dependency evidence-artifact batches
// sequentially in auto-commit transactions after the main write group has
// committed. The artifact templates MATCH the Repository endpoints the main
// batch MERGEs; on NornicDB a MATCH in a later statement of the same managed
// transaction does not see those in-transaction MERGEs, so a co-located
// artifact batch silently writes nothing while the call succeeds and the
// intents complete (#6184 run15: mains present, artifact family absent in a
// settled index, healed only by refinalize). A probe-guard retry cannot fix
// that shape — the re-run misses identically in its own transaction — so the
// statements are separated instead. Sequential auto-commit after commit is
// the same remedy #5410 (SQL relationship writes, acknowledged without
// persisting) and #4367 (repo_dependency retracts, grouped DELETE
// under-apply) already use on this backend. An artifact failure keeps the
// established contract: the error is retryable, the intents stay open, and
// the reprocessed claim re-MERGEs the mains idempotently before rewriting
// the artifacts against long-committed endpoints.
//
// It lives in its own file because edge_writer.go sits at the repository's
// 500-line cap; see edge_writer_unroutable.go for the same split precedent.
func (w *EdgeWriter) executeArtifactStatements(
	ctx context.Context,
	domain string,
	evidenceSource string,
	inputRows int,
	stmts []Statement,
	writtenRows int,
	droppedRows int,
	routeCount int,
	bs int,
) error {
	for _, stmt := range stmts {
		start := time.Now()
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
		duration := time.Since(start).Seconds()
		w.logSharedEdgeWrite(domain, evidenceSource, "artifact-sequential", inputRows, writtenRows, droppedRows, routeCount, bs, 0, duration, []Statement{stmt})
	}
	return nil
}
