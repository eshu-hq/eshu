// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// codeCallShortfallEnforced names the code-call relationship types whose
// matched-vs-submitted shortfall parks the batch instead of completing it
// (#10938). Park-on-shortfall enables per template ONLY after a census proves
// steady-state matched==submitted for that template (D6):
//
//   - CALLS is enforced: extraction drops calls whose callee did not resolve
//     (UnresolvedCalleeCount counts calls dropped, never emitted), so a
//     steady-state CALLS row references committed entities and a shortfall is
//     always the probe-to-write race — first-generation absence or a
//     non-atomic re-projection clear — never a legitimate miss.
//   - REFERENCES, USES_METACLASS, and INSTANTIATES stay count-only until
//     their census lands: #5116 documents unresolved-label endpoints that can
//     reference nodes which do not exist or carry labels outside the
//     disjunction, so a shortfall there may be a legitimate emission gap and
//     parking it would wedge the lane on unmatchable rows, trading accuracy
//     for availability. Those rows ship count+telemetry+WARN, and the
//     emission gap is a follow-up issue.
//
// The map is the census switch: flipping a template to true enrolls it in
// parking with no other code change, and only after its census evidence is
// recorded here.
var codeCallShortfallEnforced = map[string]bool{
	"CALLS":          true,
	"REFERENCES":     false,
	"USES_METACLASS": false,
	"INSTANTIATES":   false,
}

// codeCallMatchShortfallError fails a code-call batch whose enforced
// templates matched fewer rows than submitted (#10938). It mirrors
// targetMissingError: Retryable keeps the rows queued (the endpoint commits
// later in the same generation and the re-selected batch binds then), and the
// non-counting SharedEdgeTargetNotReadyFailureClass keeps the shared worker's
// re-poll — which has no attempt budget — from ever dead-lettering a timing
// state. Genuinely deleted endpoints terminate through the existing staleness
// drains, which re-evaluate before every write; no new attempt budget or TTL
// column is needed.
type codeCallMatchShortfallError struct {
	domain       string
	submitted    int
	matched      int
	relationship string
	sampleRepoID string
	sampleIntent string
}

func (e *codeCallMatchShortfallError) Error() string {
	return fmt.Sprintf(
		"%s %s batch matched %d of %d submitted rows (sample repo %q intent %q); deferring the batch until the endpoints commit",
		e.domain, e.relationship, e.matched, e.submitted, e.sampleRepoID, e.sampleIntent,
	)
}

// Retryable keeps the shortfall queued: the missing endpoint commits later
// in the same generation and the re-selected batch binds then.
func (e *codeCallMatchShortfallError) Retryable() bool { return true }

// FailureClass makes the shortfall non-counting on the reducer queue so a
// timing state never dead-letters: only the staleness drains may end the
// wait, by no longer selecting the rows.
func (e *codeCallMatchShortfallError) FailureClass() string {
	return reducer.SharedEdgeTargetNotReadyFailureClass
}

// SampleTarget names the batch's sample repository and intent ids for the
// WARN that accompanies the deferral.
func (e *codeCallMatchShortfallError) SampleTarget() (repoID, intentID string) {
	return e.sampleRepoID, e.sampleIntent
}

// codeCallCountingError fails a code-call batch the writer could not verify
// closed: the executor chain lacks the counted-write seam, a count slice came
// back short or negative, or a guard dropped a statement mid-chain. It is
// retryable but deliberately carries no failure class (mirroring
// targetProbeError): an unverifiable batch is a backend/capability fault, not
// a timing state, so a persistent fault counts toward the retry budget and
// dead-letters loudly instead of deferring forever.
type codeCallCountingError struct {
	domain string
	err    error
}

func (e *codeCallCountingError) Error() string {
	return fmt.Sprintf("%s batch could not verify matched rows: %v", e.domain, e.err)
}

// Retryable keeps the unverified batch queued rather than completing it
// silently — the exact loss #10938 closes.
func (e *codeCallCountingError) Retryable() bool { return true }

// Unwrap exposes the underlying seam failure to errors.Is/As.
func (e *codeCallCountingError) Unwrap() error { return e.err }

// writeCodeCallBatch runs a whole code-call WriteEdges batch: build the
// main-route statements, execute them through the counted-write seam, and
// reconcile matched-vs-submitted before the caller completes anything
// (#10938). It is the code_calls counterpart of WriteEdges' main-route half;
// WriteEdges delegates to it so the shared path keeps its shape for every
// other domain.
func (w *EdgeWriter) writeCodeCallBatch(
	ctx context.Context,
	evidenceSource string,
	rows []sharedintent.Row,
	routedRows map[string][]map[string]any,
	routeOrder []string,
	droppedRows int,
	report sharedintent.WriteReport,
) (sharedintent.WriteReport, error) {
	domain := reducercontract.DomainCodeCalls
	bs := w.batchSizeForDomain(domain)
	// stmtRoutes names each main-route statement's route cypher so the
	// counts reconcile per relationship template after execution.
	var stmts []sourcecypher.Statement
	var stmtRoutes []string
	for _, cypher := range routeOrder {
		routeStatements := sourcecypher.BuildEdgeRouteStatements(cypher, routedRows[cypher], bs)
		annotateEdgeStatementSummaries(domain, cypher, routeStatements)
		stmts = append(stmts, routeStatements...)
		for range routeStatements {
			stmtRoutes = append(stmtRoutes, cypher)
		}
	}
	return w.writeCodeCallMainRoutes(ctx, evidenceSource, rows, routedRows, routeOrder, stmts, stmtRoutes, bs, droppedRows, report)
}

// writeCodeCallMainRoutes executes a code-call batch's main-route statements
// with the same grouped-vs-sequential dispatch WriteEdges uses for every
// other domain, but through the counted-write seam, then reconciles
// matched-vs-submitted before the caller completes anything (#10938). The
// logging and telemetry points are identical in shape to the shared path
// (group/single labels, per-unit batch timing); only the execution seam and
// the terminal reconcile differ. stmtRoutes aligns with stmts and names each
// statement's route cypher for per-template reconciliation.
func (w *EdgeWriter) writeCodeCallMainRoutes(
	ctx context.Context,
	evidenceSource string,
	rows []sharedintent.Row,
	routedRows map[string][]map[string]any,
	routeOrder []string,
	stmts []sourcecypher.Statement,
	stmtRoutes []string,
	bs int,
	droppedRows int,
	report sharedintent.WriteReport,
) (sharedintent.WriteReport, error) {
	domain := reducercontract.DomainCodeCalls
	writtenRows := 0
	for _, cypher := range routeOrder {
		writtenRows += len(routedRows[cypher])
	}
	var matchedCounts []int64
	if _, ok := w.executor.(sourcecypher.GroupExecutor); ok {
		if groupSize := w.groupBatchSizeForDomain(domain); groupSize > 0 {
			for i := 0; i < len(stmts); i += groupSize {
				end := i + groupSize
				if end > len(stmts) {
					end = len(stmts)
				}
				start := time.Now()
				stubCtx, stubs := captureRepositoryStubs(ctx, domain)
				chunkCounts, err := executeCodeCallCountedStatements(stubCtx, w.executor, stmts[i:end])
				if err != nil {
					return report, sourcecypher.WrapRetryableNeo4jError(err)
				}
				matchedCounts = append(matchedCounts, chunkCounts...)
				duration := time.Since(start).Seconds()
				w.reportRepositoryStubs(ctx, domain, evidenceSource, stubs, stmts[i:end])
				w.recordGroupedWrite(ctx, domain, "group", duration, stmts[i:end])
				w.logSharedEdgeWrite(domain, evidenceSource, "group", len(rows), writtenRows, droppedRows, len(routeOrder), bs, groupSize, duration, stmts[i:end])
				w.recordCodeCallBatch(ctx, duration)
			}
		} else {
			start := time.Now()
			stubCtx, stubs := captureRepositoryStubs(ctx, domain)
			chunkCounts, err := executeCodeCallCountedStatements(stubCtx, w.executor, stmts)
			if err != nil {
				return report, sourcecypher.WrapRetryableNeo4jError(err)
			}
			matchedCounts = append(matchedCounts, chunkCounts...)
			duration := time.Since(start).Seconds()
			w.reportRepositoryStubs(ctx, domain, evidenceSource, stubs, stmts)
			w.recordGroupedWrite(ctx, domain, "group", duration, stmts)
			w.logSharedEdgeWrite(domain, evidenceSource, "group", len(rows), writtenRows, droppedRows, len(routeOrder), bs, 0, duration, stmts)
		}
	} else {
		for _, stmt := range stmts {
			start := time.Now()
			stubCtx, stubs := captureRepositoryStubs(ctx, domain)
			chunkCounts, err := executeCodeCallCountedStatements(stubCtx, w.executor, []sourcecypher.Statement{stmt})
			if err != nil {
				return report, sourcecypher.WrapRetryableNeo4jError(err)
			}
			matchedCounts = append(matchedCounts, chunkCounts...)
			duration := time.Since(start).Seconds()
			w.reportRepositoryStubs(ctx, domain, evidenceSource, stubs, []sourcecypher.Statement{stmt})
			w.logSharedEdgeWrite(domain, evidenceSource, "single", len(rows), writtenRows, droppedRows, len(routeOrder), bs, 0, duration, []sourcecypher.Statement{stmt})
			w.recordCodeCallBatch(ctx, duration)
		}
	}
	// An enforced shortfall returns the retryable park error here, so the
	// intents stay open.
	return reconcileCodeCallMatchedCounts(ctx, w, evidenceSource, rows, routedRows, stmts, stmtRoutes, matchedCounts, report)
}

// executeCodeCallCountedStatements runs counted-write statements through the
// executor's counted seam, preferring the atomic group path exactly where
// WriteEdges prefers ExecuteGroup, and falling back to sequential
// single-statement counts otherwise. The returned counts align with stmts.
// An executor without the seam fails closed: the batch cannot be verified, so
// it must not complete.
func executeCodeCallCountedStatements(
	ctx context.Context,
	executor sourcecypher.Executor,
	stmts []sourcecypher.Statement,
) ([]int64, error) {
	if ge, ok := executor.(sourcecypher.CountedWriteGroupExecutor); ok {
		counts, err := ge.ExecuteCountedWriteGroup(ctx, stmts)
		if err != nil {
			return nil, err
		}
		if err := sourcecypher.CountedWriteCounts(stmts, counts); err != nil {
			return nil, err
		}
		return counts, nil
	}
	if ce, ok := executor.(sourcecypher.CountedWriteExecutor); ok {
		counts := make([]int64, 0, len(stmts))
		for _, stmt := range stmts {
			count, err := ce.ExecuteCountedWrite(ctx, stmt)
			if err != nil {
				return nil, err
			}
			if count < 0 {
				return nil, fmt.Errorf("counted write returned a negative count")
			}
			counts = append(counts, count)
		}
		return counts, nil
	}
	return nil, &codeCallCountingError{
		domain: reducercontract.DomainCodeCalls,
		err:    fmt.Errorf("executor chain does not support counted writes: %w", sourcecypher.ErrCountedWriteUnsupported),
	}
}

// reconcileCodeCallMatchedCounts aggregates per-statement matched counts by
// route, records them on the report, and enforces the shortfall contract
// (#10938): an enforced-relationship shortfall parks the batch with a
// retryable non-counting error; a count-only shortfall and any surplus
// complete with WARN+telemetry so the loss is visible instead of silent.
// stmtRoutes aligns with stmts and names each statement's route cypher; row
// counts per statement come from the statements themselves, so chunking
// changes cannot skew the accounting.
func reconcileCodeCallMatchedCounts(
	ctx context.Context,
	w *EdgeWriter,
	evidenceSource string,
	rows []sharedintent.Row,
	routedRows map[string][]map[string]any,
	stmts []sourcecypher.Statement,
	stmtRoutes []string,
	counts []int64,
	report sharedintent.WriteReport,
) (sharedintent.WriteReport, error) {
	matchedByRoute := make(map[string]int64, len(routedRows))
	for i := range stmts {
		matchedByRoute[stmtRoutes[i]] += counts[i]
	}
	report.SubmittedRows = 0
	report.MatchedRows = 0
	var sampleRepoID, sampleIntentID string
	if len(rows) > 0 {
		sampleRepoID = rows[0].RepositoryID
		sampleIntentID = rows[0].IntentID
	}
	parked := ""
	for cypher, rowMaps := range routedRows {
		submitted := len(rowMaps)
		matched := matchedByRoute[cypher]
		report.SubmittedRows += submitted
		report.MatchedRows += int(matched)
		relationship := codeCallRelationshipSummary(cypher)
		switch {
		case matched < int64(submitted):
			w.reportCodeCallMatchShortfall(ctx, evidenceSource, relationship, submitted, int(matched), sampleIntentID)
			if codeCallShortfallEnforced[relationship] && parked == "" {
				parked = relationship
			}
		case matched > int64(submitted):
			w.reportCodeCallMatchSurplus(ctx, evidenceSource, relationship, submitted, int(matched), sampleIntentID)
		}
	}
	if parked != "" {
		return report, &codeCallMatchShortfallError{
			domain:       reducercontract.DomainCodeCalls,
			submitted:    report.SubmittedRows,
			matched:      report.MatchedRows,
			relationship: parked,
			sampleRepoID: sampleRepoID,
			sampleIntent: sampleIntentID,
		}
	}
	return report, nil
}

// reportCodeCallMatchShortfall emits the operator-facing signal for a
// matched-vs-submitted shortfall: a WARN naming the domain, relationship,
// and counts with a sample intent id, plus the bounded SharedEdgeTargetMiss
// counter by domain. Labels stay bounded (domain only); sample identity stays
// in the log, never in a label.
func (w *EdgeWriter) reportCodeCallMatchShortfall(
	ctx context.Context,
	evidenceSource string,
	relationship string,
	submitted int,
	matched int,
	sampleIntentID string,
) {
	if w.Instruments != nil && w.Instruments.SharedEdgeTargetMiss != nil {
		w.Instruments.SharedEdgeTargetMiss.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrDomain(reducercontract.DomainCodeCalls),
		))
	}
	if w.Logger == nil {
		return
	}
	w.Logger.WarnContext(
		ctx,
		"code call edge rows did not all match; batch held for retry",
		slog.String("domain", reducercontract.DomainCodeCalls),
		slog.String("evidence_source", evidenceSource),
		slog.String("relationship", relationship),
		slog.Int("submitted_rows", submitted),
		slog.Int("matched_rows", matched),
		slog.String("sample_intent_id", sampleIntentID),
		telemetry.PhaseAttr(telemetry.PhaseShared),
	)
}

// reportCodeCallMatchSurplus emits the operator-facing anomaly signal for a
// batch that matched MORE rows than submitted: duplicate-uid nodes in the
// graph. The batch still completes — the edges did commit — but the anomaly
// is WARNed with a sample intent id so the duplicate can be found.
func (w *EdgeWriter) reportCodeCallMatchSurplus(
	ctx context.Context,
	evidenceSource string,
	relationship string,
	submitted int,
	matched int,
	sampleIntentID string,
) {
	if w.Logger == nil {
		return
	}
	w.Logger.WarnContext(
		ctx,
		"code call edge batch matched more rows than submitted; possible duplicate uid nodes",
		slog.String("domain", reducercontract.DomainCodeCalls),
		slog.String("evidence_source", evidenceSource),
		slog.String("relationship", relationship),
		slog.Int("submitted_rows", submitted),
		slog.Int("matched_rows", matched),
		slog.String("sample_intent_id", sampleIntentID),
		telemetry.PhaseAttr(telemetry.PhaseShared),
	)
}
