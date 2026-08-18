// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"time"
)

// buildRationaleRowMap routes a rationale EXPLAINS edge row (issue #2230) to its
// template. The source is an identity-only Rationale node; the target is the
// code entity the intent comment precedes, matched by uid.
func buildRationaleRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	rationaleUID := payloadString(payload, "rationale_uid")
	targetEntityID := payloadString(payload, "target_entity_id")
	if rationaleUID == "" || targetEntityID == "" {
		return "", nil, false
	}

	rowMap := map[string]any{
		"rationale_uid":    rationaleUID,
		"target_entity_id": targetEntityID,
		"repo_id":          payloadString(payload, "repo_id"),
		"comment_kind":     payloadString(payload, "comment_kind"),
		"excerpt_hash":     payloadString(payload, "excerpt_hash"),
		"evidence_source":  evidenceSource,
	}
	return batchCanonicalRationaleExplainsEdgeCypher, rowMap, true
}

// BuildRetractRationaleEdges builds a repo-scoped EXPLAINS edge retraction
// statement.
func BuildRetractRationaleEdges(repoIDs []string, evidenceSource string) Statement {
	sources := rationaleRetractEvidenceSources(evidenceSource)
	if len(sources) > 1 {
		return Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    retractCanonicalRationaleEdgesCypher,
			Parameters: map[string]any{
				"repo_ids":         repoIDs,
				"evidence_sources": sources,
			},
		}
	}
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractRationaleEdgesCypher,
		Parameters: map[string]any{
			"repo_ids":        repoIDs,
			"evidence_source": evidenceSource,
		},
	}
}

// BuildProbeRationaleEdges builds a read-only statement that answers whether
// BuildRetractRationaleEdges' whole-repository retract, for the identical
// repoIDs and evidenceSource, would remove anything. It MUST mirror whichever
// of the two retract shapes BuildRetractRationaleEdges picks (multi-source
// combined canonical-plus-legacy, or single-source exact match) so the probe
// and the retract it guards always ask the same question (#5998). Both
// branches carry OperationCanonicalProbe, not OperationCanonicalRetract: that
// label feeds BackpressureGate.Acquire's wait-metric attribute, and this guard
// runs one probe per RetractEdges batch whether or not the paired DELETE
// follows, so mislabelling it as a retract would count every probe as a
// retract and mask the ratio of probes to real deletes -- the very signal the
// guard adds. It does not reach the retry counter: ExecuteProbe does not
// retry.
func BuildProbeRationaleEdges(repoIDs []string, evidenceSource string) Statement {
	sources := rationaleRetractEvidenceSources(evidenceSource)
	if len(sources) > 1 {
		return Statement{
			Operation: OperationCanonicalProbe,
			Cypher:    probeCanonicalRationaleEdgesCypher,
			Parameters: map[string]any{
				"repo_ids":         repoIDs,
				"evidence_sources": sources,
			},
		}
	}
	return Statement{
		Operation: OperationCanonicalProbe,
		Cypher:    probeRationaleEdgesCypher,
		Parameters: map[string]any{
			"repo_ids":        repoIDs,
			"evidence_source": evidenceSource,
		},
	}
}

// rationaleProbeOutcome is the bounded `outcome` label on
// eshu_dp_rationale_retract_probe_outcomes_total. It is a named type rather
// than a bare string because the value reaches a metric attribute: an
// unconstrained string parameter lets a caller put anything -- an evidence
// source, a repo id -- into a label and blow up its cardinality.
//
// Be clear about how much the type buys, because it is less than it looks: Go
// converts an untyped string CONSTANT to this type implicitly, so a literal at
// the call site still compiles. The type stops a typed `string` variable from
// being passed; what actually caught the real mistake -- a test handing an
// evidence source in as the scope -- was asserting the emitted labels back in
// edge_writer_logging_test.go. Keep that assertion.
type rationaleProbeOutcome string

// Outcome values for the rationale retract probe guard (#5998). Kept as a
// bounded enum, not free text, so the metric label stays low cardinality and
// the structured log/span event (edge_writer_logging.go's
// observeRationaleRetractProbe) agree with it exactly.
const (
	rationaleRetractProbeOutcomeSkipped     rationaleProbeOutcome = "skipped"     // probe found zero rows; DELETE not executed
	rationaleRetractProbeOutcomeDeleted     rationaleProbeOutcome = "deleted"     // probe found rows; DELETE executed
	rationaleRetractProbeOutcomeUnsupported rationaleProbeOutcome = "unsupported" // executor has no ProbeExecutor; DELETE executed unconditionally
	rationaleRetractProbeOutcomeProbeError  rationaleProbeOutcome = "probe_error" // probe returned an error; DELETE executed unconditionally
)

// retractRationaleEdgesWithProbe implements the #5998 probe-then-delete guard
// for the whole-repository rationale EXPLAINS retract (RetractEdges,
// DomainRationaleEdges branch). On the pinned NornicDB build the shipped
// DELETE costs roughly 18s per statement even when it deletes zero rows
// (ledger:5998-zero-row-explains-delete-large-store), because its cost tracks
// store size rather than rows deleted, while the identical MATCH run as a read
// stays at roughly 21ms on the same store. That
// 21ms figure timed the pre-change RETURN rel shape
// (ledger:5998-explains-existence-probe-read); the shipped RETURN true LIMIT 1
// returns a literal instead of serializing a relationship and was never timed
// separately, so the figure bounds it from above rather than measuring it.
// The probe runs with the SAME repoIDs and evidenceSource as the DELETE it
// guards, so the two always bind the identical set.
//
// Granularity is per RetractEdges BATCH, not per repository. The reducer
// selects intents by partition-hash bucket (defaultPartitionCount=8) up to
// defaultBatchLimit (100) rows, planRepoWideRetractWork routes every
// non-skipped refresh row into one retract set, and the worker makes a single
// RetractEdges call for it -- so one probe and at most one DELETE cover every
// repository in that batch, binding all of their ids in $repo_ids at once.
// Two consequences worth holding onto: a corpus of ~900 repositories issues on
// the order of 9-16 whole-scope statements per generation rather than ~900,
// and a SKIP requires zero EXPLAINS edges across every repository in the
// batch, so one repository with a single rationale edge makes the batch-wide
// DELETE run for all of them. RetractEdges' mixed-batch path
// passes every repository in the batch whose row is a whole-scope REFRESH row,
// meaning it carries the refresh intent_type and no delta_projection (see
// collectWholeScopeRefreshRepoIDs for why both conditions are required, and
// what an over-broad collector would delete). Both are safe for the same
// reason -- probe and delete bind the same set -- but this is not a
// one-repository-per-statement guarantee.
//
// Edge writes carrying the retract_via_refresh marker are fenced behind this
// same refresh, so a marked writer cannot insert EXPLAINS edges between probe
// and delete; unmarked legacy rows bypass that fence, with the residual
// documented on executeGuardedRationaleDeltaRetracts.
//
// Fail-safe direction is non-negotiable: when the executor does not implement
// ProbeExecutor, or the probe itself errors, this runs the DELETE
// unconditionally, exactly as before the probe guard existed. A skipped
// delete can leave stale graph state; a redundant delete only costs time. Only
// a probe that definitively reports zero rows skips the delete.
func (w *EdgeWriter) retractRationaleEdgesWithProbe(
	ctx context.Context,
	repoIDs []string,
	evidenceSource string,
) error {
	deleteStmt := BuildRetractRationaleEdges(repoIDs, evidenceSource)

	pe, ok := w.executor.(ProbeExecutor)
	if !ok {
		w.observeRationaleRetractProbe(ctx, rationaleRetractProbeOutcomeUnsupported, repoIDs, rationaleWholeScopeProbeScope, 0, nil)
		return WrapRetryableNeo4jError(w.executor.Execute(ctx, deleteStmt))
	}

	probeStmt := BuildProbeRationaleEdges(repoIDs, evidenceSource)
	start := time.Now()
	found, err := pe.ExecuteProbe(ctx, probeStmt)
	duration := time.Since(start).Seconds()
	if err != nil {
		w.observeRationaleRetractProbe(ctx, rationaleRetractProbeOutcomeProbeError, repoIDs, rationaleWholeScopeProbeScope, duration, err)
		return WrapRetryableNeo4jError(w.executor.Execute(ctx, deleteStmt))
	}
	if !found {
		w.observeRationaleRetractProbe(ctx, rationaleRetractProbeOutcomeSkipped, repoIDs, rationaleWholeScopeProbeScope, duration, nil)
		return nil
	}

	w.observeRationaleRetractProbe(ctx, rationaleRetractProbeOutcomeDeleted, repoIDs, rationaleWholeScopeProbeScope, duration, nil)
	return WrapRetryableNeo4jError(w.executor.Execute(ctx, deleteStmt))
}

// BuildRetractRationaleEdgeStatementsByFilePath builds per-target-label
// EXPLAINS edge retraction statements for target code entities owned by the
// given repo-qualified paths.
//
// A single statement cannot bind all target labels on NornicDB: a bare MATCH
// whose target carries a node-label disjunction matches zero rows on v1.1.11
// (probed — the combined statement deleted nothing while these per-label
// statements deleted every edge), so the retract fans out to one statement per
// label in rationaleExplainsTargetLabels, the same list the write template's
// disjunction is built from. The statements run sequentially, each in its own
// transaction, for the managed-transaction reason documented on
// executeCodeCallRetractStatements.
func BuildRetractRationaleEdgeStatementsByFilePath(filePaths []string, evidenceSource string) []Statement {
	evidencePredicate, evidenceParameters := rationaleDeltaEvidencePredicate(evidenceSource)

	return buildRationaleDeltaStatementsByFilePath(
		filePaths, evidencePredicate, evidenceParameters,
		OperationCanonicalRetract, rationaleDeltaRetractTerminalClause,
	)
}

// BuildProbeRationaleEdgeStatementsByFilePath builds the read-only existence
// probes that guard BuildRetractRationaleEdgeStatementsByFilePath's per-label
// deletes, one probe per label, in the same order.
//
// Each delta statement matches a different target label, so a single shared
// probe would answer a narrower question than six of the seven deletes it
// guarded; the probe therefore fans out exactly as the retract does and
// differs only in its terminal clause. Both take their label list from
// buildRationaleDeltaStatementsByFilePath and their evidence predicate AND its
// bound parameters from rationaleDeltaEvidencePredicate, so a change to either
// reaches both. That shared derivation is what the drift test asserts, and it
// asserts Parameters as well as Cypher: an evidence predicate that drifted only
// in its bound value would emit byte-identical Cypher while the probe asked a
// narrower question than the delete it guards.
//
// The guard exists because this shape carries the same store-size term as the
// whole-repository retract: seven statements cost about 12s together on a
// 190,000-relationship store while deleting zero rows, against about 0.29s on
// an empty one (ledger:5998-delta-per-label-retract-seeded-rerun,
// ledger:5998-delta-per-label-retract-empty). Same host and image, but the
// empty half is from an earlier session than the seeded figure -- the evidence
// doc records which and why the direction is conservative. That pair is what
// proves the cost tracks store size rather than rows removed; it is not
// compared against the whole-repository figures, which were measured on a
// different host and store. The seven probes that guard these deletes cost
// 0.31s and do not grow with the store
// (ledger:5998-delta-per-label-probe-seeded,
// ledger:5998-delta-per-label-probe-empty). Unlike the whole-repository
// refresh, this path runs on every incremental sync.
func BuildProbeRationaleEdgeStatementsByFilePath(filePaths []string, evidenceSource string) []Statement {
	evidencePredicate, evidenceParameters := rationaleDeltaEvidencePredicate(evidenceSource)

	return buildRationaleDeltaStatementsByFilePath(
		filePaths, evidencePredicate, evidenceParameters,
		OperationCanonicalProbe, rationaleDeltaProbeTerminalClause,
	)
}

// rationaleProbeScope is the bounded `scope` label on
// eshu_dp_rationale_retract_probe_outcomes_total, a named type for the same
// reason rationaleProbeOutcome is.
type rationaleProbeScope string

// Bounded values for the probe guard's `scope` label. Exactly two, so the
// metric stays low cardinality while still separating the two guarded paths:
// the whole-scope refresh fires one statement per RetractEdges batch,
// the delta path fires one per target label on every incremental sync.
const (
	rationaleWholeScopeProbeScope rationaleProbeScope = "whole_scope"
	rationaleDeltaProbeScope      rationaleProbeScope = "delta_by_file_path"
)

// Terminal clauses for the paired per-label delta statements. They are the only
// difference between a delta retract and its probe; everything above them is
// built once in buildRationaleDeltaStatementsByFilePath.
const (
	rationaleDeltaRetractTerminalClause = "DELETE rel"
	rationaleDeltaProbeTerminalClause   = "RETURN true LIMIT 1"
)

// rationaleDeltaEvidencePredicate returns the evidence-source WHERE fragment and
// its bound parameters for the per-label delta statements. The retract and its
// probe both call this rather than each computing it, because the predicate and
// the value it binds must stay identical: a probe that bound a narrower source
// set than its paired delete would emit byte-identical Cypher and still answer a
// different question, skipping a delete that had rows to remove.
func rationaleDeltaEvidencePredicate(evidenceSource string) (string, map[string]any) {
	sources := rationaleRetractEvidenceSources(evidenceSource)
	if len(sources) > 1 {
		return "rel.evidence_source IN $evidence_sources", map[string]any{"evidence_sources": sources}
	}
	return "rel.evidence_source = $evidence_source", map[string]any{"evidence_source": evidenceSource}
}

// buildRationaleDeltaStatementsByFilePath builds one statement per entry in
// rationaleExplainsTargetLabels, identical up to the terminal clause. Both the
// delta retract and its probe go through here so a change to the MATCH, the
// path predicate, or the evidence predicate reaches both at once.
func buildRationaleDeltaStatementsByFilePath(
	filePaths []string,
	evidencePredicate string,
	evidenceParameters map[string]any,
	operation Operation,
	terminalClause string,
) []Statement {
	stmts := make([]Statement, 0, len(rationaleExplainsTargetLabels))
	for _, label := range rationaleExplainsTargetLabels {
		cypher := "MATCH (rationale:Rationale)-[rel:EXPLAINS]->(target:" + label + ")\n" +
			"WHERE target.path IN $file_paths\n" +
			"  AND " + evidencePredicate + "\n" +
			terminalClause
		parameters := map[string]any{"file_paths": filePaths}
		for key, value := range evidenceParameters {
			parameters[key] = value
		}
		stmts = append(stmts, Statement{
			Operation:  operation,
			Cypher:     cypher,
			Parameters: parameters,
		})
	}
	return stmts
}
