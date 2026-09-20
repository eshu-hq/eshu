// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"fmt"
	"strings"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Bounded reason values for the SharedEdgeUnroutableRows counter. Keeping the
// set closed means the label stays low-cardinality; the domain, evidence
// source, and a sample intent id travel in the structured log instead.
const (
	unroutableReasonPartialBatch = "partial_batch"
	unroutableReasonWholeBatch   = "whole_batch"
)

// reportUnroutableRows emits the operator-facing signal for rows a shared-edge
// write could not route: a WARN structured log naming the domain, the counts,
// and one sample intent id an operator can look up, plus the bounded
// SharedEdgeUnroutableRows counter. wholeBatch distinguishes a partial loss
// (the routable rows still wrote) from a batch that produced nothing.
func (w *EdgeWriter) reportUnroutableRows(
	ctx context.Context,
	domain string,
	evidenceSource string,
	inputRows int,
	droppedRows int,
	sampleIntentID string,
	wholeBatch bool,
) {
	reason := unroutableReasonPartialBatch
	if wholeBatch {
		reason = unroutableReasonWholeBatch
	}

	if w.Instruments != nil && w.Instruments.SharedEdgeUnroutableRows != nil {
		w.Instruments.SharedEdgeUnroutableRows.Add(ctx, int64(droppedRows), metric.WithAttributes(
			telemetry.AttrDomain(domain),
			telemetry.AttrReason(reason),
		))
	}

	if w.Logger == nil {
		return
	}
	w.Logger.Warn(
		"shared edge rows unroutable",
		"domain", domain,
		"evidence_source", evidenceSource,
		"input_rows", inputRows,
		"dropped_rows", droppedRows,
		"reason", reason,
		"sample_intent_id", sampleIntentID,
	)
}

// unroutableReasonForRow classifies WHY a row could not be routed, so an
// operator can tell a genuine data loss from version skew.
//
// The distinction is not cosmetic. A row missing its MATCH identifiers can
// never become an edge under any writer version, and the loss is real. A row
// whose relationship type has no statement in THIS binary is well formed and
// would route once the writer catches up — during a rolling upgrade a newer
// producer emits types an older writer has never heard of. Recording both as
// the same thing would either hide real losses in deploy noise or page someone
// about a rollout that is working as intended.
func unroutableReasonForRow(domain string, row reducer.SharedProjectionIntentRow) string {
	if domain != reducer.DomainRepoDependency {
		return reducer.UnroutableReasonMissingRequiredField
	}
	// repo_dependency is the only domain today whose rejection can mean "this
	// writer has no statement for that type" rather than "the payload is
	// incomplete": it looks the relationship type up in a table
	// (BatchCanonicalTypedRepoRelationshipUpsertCypher). Every other domain
	// rejects only on empty required fields.
	relationshipType := sourcecypher.PayloadString(row.Payload, "relationship_type")
	if relationshipType == "" || relationshipType == string(edgetype.DependsOn) {
		return reducer.UnroutableReasonMissingRequiredField
	}
	if sourcecypher.PayloadString(row.Payload, "repo_id") == "" || sourcecypher.PayloadString(row.Payload, "target_repo_id") == "" {
		return reducer.UnroutableReasonMissingRequiredField
	}
	if _, ok := sourcecypher.BatchCanonicalTypedRepoRelationshipUpsertCypher(relationshipType); !ok {
		return reducer.UnroutableReasonNoStatementForType
	}
	return reducer.UnroutableReasonMissingRequiredField
}

// Write-target existence guard (#6184). It shares this file with the
// unroutable sink because both answer the same operator question — "the
// batch did not produce edges, was anything lost?" — with opposite
// dispositions: unroutable rows complete loudly, while rows whose runtime
// target is merely absent stay queued for a retry.
//
// The symbol-to-runtime batch writers resolve their edge targets with a Cypher
// MATCH: HANDLES_ROUTE binds (repo_id, path) :Endpoint nodes committed by
// workload materialization, RUNS_IN binds the :Workload nodes a repository
// DEFINES. When the MATCH finds no node the MERGE is a silent no-op, which is
// the correct defense-in-depth against fabricating edges — but the batch then
// succeeds, the worker completes the intents, and the edge is lost with no
// error and no dead letter.
//
// That silence is only safe when a miss is impossible. It is not: the
// endpoint-presence gate proves the target committed in Postgres, while a
// rebuild that wiped the graph (or a materialization pass that published its
// readiness phase before recommitting the nodes) can leave the graph without
// it when the legs drain. The presence row survives in Postgres across the
// wipe, so the legs legitimately proceed — and then bind nothing. Retrying is
// always productive here: the presence row proves these facts derive the
// target, so a later materialization pass in the same generation commits it
// and the re-selected batch binds.
//
// The guard therefore probes, per write batch, whether every row's runtime
// target exists, and fails the batch with a retryable error on the first
// miss. The worker treats a WriteEdges error as a failed cycle: nothing is
// completed, the rows stay open, and the next cycle re-selects them. A
// A failing existence probe (the backend did not answer) defers the batch
// retryably like a detected miss: writing unchecked would recreate the exact
// silent zero-edge loss the guard exists to prevent whenever the probe fails
// while a target is actually absent. An executor without probe capability
// keeps today's behavior byte-identically.
//
// Deliberately scoped to the presence-gated MATCH-dependent domains. The
// presence gate already terminalizes route-only legs before they reach the
// write, so a miss here can never be a legitimate route-only row, and a retry
// can never stall one. Other domains keep their existing contract; see the
// per-template comments for why their MATCH misses stay silent.

// targetMissProbeDomains are the shared-projection domains whose batch write
// resolves a cross-acceptance-unit runtime target committed by a different
// handler, and whose rows therefore reach WriteEdges only after a presence
// gate proved the target committable. A miss for these rows is always worth
// a retry, never a silent completion.
var targetMissProbeDomains = map[string]bool{
	reducer.DomainHandlesRoute:        true,
	reducer.DomainRunsIn:              true,
	reducer.DomainDeployableUnitEdges: true,
}

// buildTargetPresenceProbeStatement returns the existence probe for one
// routed batch, or false for domains outside the guard. The probe returns a
// row if and only if every distinct runtime target of the batch exists, so
// the ProbeExecutor boolean is the verdict directly: found means write,
// not-found means a target is absent and the batch must be deferred.
//
// Shape discipline (docs/public/reference/nornicdb-query-pitfalls.md): one
// inline-anchored MATCH clause per distinct target, no WITH, no OPTIONAL
// MATCH, no subquery — sequential anchored MATCHes over literal parameters.
// Correlated COUNT{}/EXISTS{} subqueries over UNWIND rows are rejected by
// the backend, and a WHERE attached to a WITH is not evaluated as a filter,
// so neither can carry this verdict. The probe shape is proven live in both
// directions on the pinned backend (see the #6184 run notes); the unit tests
// pin the builder output.
func buildTargetPresenceProbeStatement(domain string, rows []map[string]any) (sourcecypher.Statement, bool) {
	if !targetMissProbeDomains[domain] {
		return sourcecypher.Statement{}, false
	}
	params := make(map[string]any, 2*len(rows))
	var sb strings.Builder
	clause := 0
	seen := make(map[string]struct{}, len(rows))
	emit := func(key string, match string, kv ...any) {
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		// The clause indexes in match and kv are bound at the call site from
		// the pre-increment counter, so every MATCH clause gets a unique
		// parameter pair.
		sb.WriteString("MATCH " + match + "\n")
		for i := 0; i < len(kv); i += 2 {
			params[kv[i].(string)] = kv[i+1]
		}
		clause++
	}
	switch domain {
	case reducer.DomainHandlesRoute:
		// The write MATCHes both (f:Function {uid}) and (e:Endpoint
		// {repo_id, path}); the probe anchors both, or a batch whose
		// Function node is absent (post-wipe ordering vs content
		// projection) passes the guard and completes a silent zero-edge
		// write (#6730 owner finding).
		for _, row := range rows {
			repo, _ := row["repo_id"].(string)
			path, _ := row["path"].(string)
			if repo == "" || path == "" {
				continue
			}
			key := repo + "\x00" + path
			emit(key, fmt.Sprintf("(e%d:Endpoint {repo_id: $r%d, path: $p%d})", clause, clause, clause),
				fmt.Sprintf("r%d", clause), repo, fmt.Sprintf("p%d", clause), path)
			if uid, _ := row["function_entity_id"].(string); uid != "" {
				emit("f\x00"+uid, fmt.Sprintf("(f%d:Function {uid: $u%d})", clause, clause),
					fmt.Sprintf("u%d", clause), uid)
			}
		}
	case reducer.DomainRunsIn:
		for _, row := range rows {
			repo, _ := row["repo_id"].(string)
			if repo == "" {
				continue
			}
			emit(repo, fmt.Sprintf("(:Repository {id: $r%d})-[:DEFINES]->(:Workload)", clause),
				fmt.Sprintf("r%d", clause), repo)
		}
	case reducer.DomainDeployableUnitEdges:
		// The deployable-unit template MATCHes both endpoint Repositories;
		// the deployment node is committed by another scope's
		// materialization with no happens-before against this batch (#6184).
		for _, row := range rows {
			if repo, _ := row["repo_id"].(string); repo != "" {
				emit("s\x00"+repo, fmt.Sprintf("(:Repository {id: $s%d})", clause),
					fmt.Sprintf("s%d", clause), repo)
			}
			if repo, _ := row["deployment_repo_id"].(string); repo != "" {
				emit("t\x00"+repo, fmt.Sprintf("(:Repository {id: $t%d})", clause),
					fmt.Sprintf("t%d", clause), repo)
			}
		}
	default:
		return sourcecypher.Statement{}, false
	}
	if clause == 0 {
		return sourcecypher.Statement{}, false
	}
	sb.WriteString("RETURN 1 LIMIT 1")
	return sourcecypher.Statement{
		Operation:  sourcecypher.OperationCanonicalProbe,
		Cypher:     sb.String(),
		Parameters: params,
	}, true
}

// targetMissingError fails a batch whose runtime target is absent from
// the graph. Retryable() keeps the rows queued: the target commits later in
// the same generation (its presence row proves these facts derive it) and
// the re-selected batch binds then. It must never be terminalized into an
// unroutable completion — an absent target here is a timing state, not a
// payload defect.
type targetMissingError struct {
	domain         string
	batchRows      int
	sampleRepoID   string
	sampleIntentID string
}

func (e *targetMissingError) Error() string {
	return fmt.Sprintf(
		"%s batch of %d row(s) has no graph target (sample repo %q intent %q); deferring the batch until materialization commits the target",
		e.domain, e.batchRows, e.sampleRepoID, e.sampleIntentID,
	)
}

// Retryable opts the miss into bounded queue retries.
func (e *targetMissingError) Retryable() bool { return true }

// targetProbeError fails a batch whose target-existence probe could not
// run. Retryable() keeps the rows queued on the same non-counting class as a
// detected miss: writing unchecked would recreate the exact silent zero-edge
// loss the guard exists to prevent whenever the probe fails (timeout or
// rejection under load) while a target is actually absent (#6730 Codex P1).
type targetProbeError struct {
	domain         string
	batchRows      int
	sampleIntentID string
	err            error
}

func (e *targetProbeError) Error() string {
	return fmt.Sprintf(
		"%s batch of %d row(s) target-existence probe failed (sample intent %q); deferring the unverified batch rather than writing unchecked: %v",
		e.domain, e.batchRows, e.sampleIntentID, e.err,
	)
}

// Retryable opts the probe failure into bounded queue retries.
func (e *targetProbeError) Retryable() bool { return true }

// Unwrap exposes the probe failure to errors.Is/As without changing the
// retryable failure class.
func (e *targetProbeError) Unwrap() error { return e.err }

// checkBatchTargetsPresent probes one routed batch for runtime-target
// completeness before its write statements run. It returns nil when the
// domain is unguarded, the executor has no probe capability, or every target
// is present. A probe failure or a detected miss emits the operator signal
// and returns a retryable error; the caller must run no write statement for
// the batch afterwards.
func (w *EdgeWriter) checkBatchTargetsPresent(
	ctx context.Context,
	prober sourcecypher.ProbeExecutor,
	domain string,
	evidenceSource string,
	rows []map[string]any,
	sampleRepoID string,
	sampleIntentID string,
) error {
	probe, ok := buildTargetPresenceProbeStatement(domain, rows)
	if !ok {
		return nil
	}
	allPresent, err := prober.ExecuteProbe(ctx, probe)
	if err != nil {
		if w.Logger != nil {
			w.Logger.Warn(
				"shared edge target probe failed, deferring unverified batch",
				"domain", domain,
				"evidence_source", evidenceSource,
				"batch_rows", len(rows),
				"sample_intent_id", sampleIntentID,
				"error", err,
			)
		}
		return &targetProbeError{
			domain:         domain,
			batchRows:      len(rows),
			sampleIntentID: sampleIntentID,
			err:            err,
		}
	}
	if allPresent {
		return nil
	}
	if w.Instruments != nil && w.Instruments.SharedEdgeTargetMiss != nil {
		w.Instruments.SharedEdgeTargetMiss.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrDomain(domain),
		))
	}
	if w.Logger != nil {
		w.Logger.Warn(
			"shared edge batch target absent, deferring batch",
			"domain", domain,
			"evidence_source", evidenceSource,
			"batch_rows", len(rows),
			"sample_repo_id", sampleRepoID,
			"sample_intent_id", sampleIntentID,
		)
	}
	return &targetMissingError{
		domain:         domain,
		batchRows:      len(rows),
		sampleRepoID:   sampleRepoID,
		sampleIntentID: sampleIntentID,
	}
}

// checkRoutedBatchTargets runs the target-presence guard over every routed
// group of one WriteEdges call, chunked at the same batch size the write
// path uses (see EdgeWriter.batchSizeForDomain): one probe covers at most
// one write batch worth of rows, so a large drain cannot build an unbounded
// MATCH chain the backend never proved — the fence scales down with the
// batch size (#6184 owner finding on PR #6730). The sample intent identifies
// the batch in the log; per-group samples would cost an intent-id index
// through routing for no operator benefit, so the batch head stands in for
// all groups.
func checkRoutedBatchTargets(
	ctx context.Context,
	w *EdgeWriter,
	domain string,
	evidenceSource string,
	rows []reducer.SharedProjectionIntentRow,
	routedRows map[string][]map[string]any,
	routeOrder []string,
) error {
	prober, ok := w.executor.(sourcecypher.ProbeExecutor)
	if !ok {
		return nil
	}
	var sampleRepoID, sampleIntentID string
	if len(rows) > 0 {
		sampleRepoID = rows[0].RepositoryID
		sampleIntentID = rows[0].IntentID
	}
	bs := w.batchSizeForDomain(domain)
	for _, cypher := range routeOrder {
		group := routedRows[cypher]
		if bs <= 0 || bs >= len(group) {
			if err := w.checkBatchTargetsPresent(ctx, prober, domain, evidenceSource, group, sampleRepoID, sampleIntentID); err != nil {
				return err
			}
			continue
		}
		for start := 0; start < len(group); start += bs {
			end := start + bs
			if end > len(group) {
				end = len(group)
			}
			if err := w.checkBatchTargetsPresent(ctx, prober, domain, evidenceSource, group[start:end], sampleRepoID, sampleIntentID); err != nil {
				return err
			}
		}
	}
	return nil
}

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
func (w *EdgeWriter) executeArtifactStatements(
	ctx context.Context,
	domain string,
	evidenceSource string,
	inputRows int,
	stmts []sourcecypher.Statement,
	writtenRows int,
	droppedRows int,
	routeCount int,
	bs int,
) error {
	// One summary entry for the whole artifact phase: per-statement entries
	// each carrying the full claim's input_intents overcount k× for a claim
	// with k artifact statements, while the phase totals (executed_rows,
	// statement_count) stay summable (#6730 owner finding). The grouped-write
	// instruments record the phase under execution_mode="artifact-sequential"
	// so they keep covering the whole repo_dependency write.
	totalDuration := 0.0
	for _, stmt := range stmts {
		start := time.Now()
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return sourcecypher.WrapRetryableNeo4jError(err)
		}
		totalDuration += time.Since(start).Seconds()
	}
	if len(stmts) == 0 {
		return nil
	}
	w.recordGroupedWrite(ctx, domain, "artifact-sequential", totalDuration, stmts)
	w.logSharedEdgeWrite(domain, evidenceSource, "artifact-sequential", inputRows, writtenRows, droppedRows, routeCount, bs, 0, totalDuration, stmts)
	return nil
}
