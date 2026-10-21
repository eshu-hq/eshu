// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Closed outcome values for the DeployableUnitEdgeVerify counter, the span
// attribute, and the WARN verdict.
const (
	edgeVerifyPresent       = "present"
	edgeVerifyMissingTarget = "missing_target"
	edgeVerifyMissingEdge   = "missing_edge"
	edgeVerifyProbeError    = "probe_error"
)

// deployableUnitEdgeProbeCypher reads one CORRELATES_DEPLOYABLE_UNIT edge.
// It is a single pattern: one MATCH, both endpoints anchored on the unique
// Repository.id, and the relationship property in WHERE rather than inline
// (docs/public/reference/nornicdb-pitfalls.md). The multi-clause shape of
// the pre-write target probe is deliberately not reused (#8315).
const deployableUnitEdgeProbeCypher = "MATCH (:Repository {id: $source_repo_id})-[rel:CORRELATES_DEPLOYABLE_UNIT]->(:Repository {id: $target_repo_id})\n" +
	"WHERE rel.evidence_source = $evidence_source\nRETURN 1 LIMIT 1"

// repositoryPresenceProbeCypher reads one Repository node by its unique id.
const repositoryPresenceProbeCypher = "MATCH (:Repository {id: $repo_id})\nRETURN 1 LIMIT 1"

func buildDeployableUnitEdgeProbe(sourceRepoID, targetRepoID, evidenceSource string) sourcecypher.Statement {
	return sourcecypher.Statement{
		Operation: sourcecypher.OperationCanonicalProbe,
		Cypher:    deployableUnitEdgeProbeCypher,
		Parameters: map[string]any{
			"source_repo_id":  sourceRepoID,
			"target_repo_id":  targetRepoID,
			"evidence_source": evidenceSource,
		},
	}
}

func buildRepositoryPresenceProbe(repoID string) sourcecypher.Statement {
	return sourcecypher.Statement{
		Operation:  sourcecypher.OperationCanonicalProbe,
		Cypher:     repositoryPresenceProbeCypher,
		Parameters: map[string]any{"repo_id": repoID},
	}
}

// deployableUnitEdgeUnverifiedError fails a deployable_unit_correlation run
// whose edge write reported success while the edge is absent and both
// endpoint Repository nodes exist. That is not a timing state, so the class
// counts toward the retry budget and a repeat dead-letters loudly.
type deployableUnitEdgeUnverifiedError struct {
	sourceRepoID   string
	targetRepoID   string
	sampleIntentID string
}

func (e *deployableUnitEdgeUnverifiedError) Error() string {
	return fmt.Sprintf(
		"CORRELATES_DEPLOYABLE_UNIT edge %q -> %q is absent after a successful write although both Repository nodes exist (sample intent %q)",
		e.sourceRepoID, e.targetRepoID, e.sampleIntentID,
	)
}

// Retryable keeps the run queued; the counting class bounds the retries.
func (e *deployableUnitEdgeUnverifiedError) Retryable() bool { return true }

// FailureClass tags the error with the counting
// reducer.DeployableUnitEdgeWriteUnverifiedFailureClass.
func (e *deployableUnitEdgeUnverifiedError) FailureClass() string {
	return reducer.DeployableUnitEdgeWriteUnverifiedFailureClass
}

// VerifyDeployableUnitEdges proves every routable admitted
// CORRELATES_DEPLOYABLE_UNIT row is present in the graph after WriteEdges
// returned (#8283, verified success). The canonical upsert MATCHes both
// endpoint Repository nodes, so an endpoint committed after the write leaves
// the MERGE with nothing to bind and the write still reports success.
//
// It runs one single-pattern edge probe per distinct (source, target) pair and
// stops at the first edge that is not present. On a miss it probes the two
// endpoint nodes: an absent endpoint returns the non-counting
// targetMissingError (the #7268 elapsed bound still applies in the handler);
// two present endpoints return the counting deployableUnitEdgeUnverifiedError.
// A probe failure returns the counting targetProbeError. An executor without
// probe capability returns reducer.ErrDeployableUnitEdgeVerificationUnavailable.
// Rows the write path cannot route are skipped: they were never written.
// It implements reducer.DeployableUnitEdgeVerifier.
func (w *EdgeWriter) VerifyDeployableUnitEdges(
	ctx context.Context,
	rows []sharedintent.Row,
	evidenceSource string,
) error {
	prober, ok := w.executor.(sourcecypher.ProbeExecutor)
	if !ok {
		return reducer.ErrDeployableUnitEdgeVerificationUnavailable
	}
	seen := make(map[[2]string]struct{}, len(rows))
	present := 0
	for _, row := range rows {
		source, target, routable := deployableUnitRowEndpoints(row.Payload)
		if !routable {
			continue
		}
		pair := [2]string{source, target}
		if _, dup := seen[pair]; dup {
			continue
		}
		seen[pair] = struct{}{}
		outcome, absentRepoID, err := probeDeployableUnitEdge(ctx, prober, pair, evidenceSource)
		w.recordEdgeVerify(ctx, outcome)
		if outcome == edgeVerifyPresent {
			present++
			continue
		}
		w.finishEdgeVerify(ctx, outcome, present, len(seen))
		w.warnEdgeVerify(outcome, pair, row.IntentID, evidenceSource, present, err)
		switch outcome {
		case edgeVerifyMissingTarget:
			return &targetMissingError{
				domain:         reducercontract.DomainDeployableUnitEdges,
				batchRows:      len(rows),
				sampleRepoID:   absentRepoID,
				sampleIntentID: row.IntentID,
			}
		case edgeVerifyMissingEdge:
			return &deployableUnitEdgeUnverifiedError{
				sourceRepoID:   pair[0],
				targetRepoID:   pair[1],
				sampleIntentID: row.IntentID,
			}
		default:
			return &targetProbeError{
				domain:         reducercontract.DomainDeployableUnitEdges,
				batchRows:      len(rows),
				sampleIntentID: row.IntentID,
				err:            err,
			}
		}
	}
	w.finishEdgeVerify(ctx, edgeVerifyPresent, present, len(seen))
	return nil
}

// probeDeployableUnitEdge returns the verdict for one edge and, for
// missing_target, the id of the absent endpoint.
func probeDeployableUnitEdge(
	ctx context.Context,
	prober sourcecypher.ProbeExecutor,
	pair [2]string,
	evidenceSource string,
) (outcome string, absentRepoID string, err error) {
	found, err := prober.ExecuteProbe(ctx, buildDeployableUnitEdgeProbe(pair[0], pair[1], evidenceSource))
	if err != nil {
		return edgeVerifyProbeError, "", err
	}
	if found {
		return edgeVerifyPresent, "", nil
	}
	for _, repoID := range pair {
		exists, err := prober.ExecuteProbe(ctx, buildRepositoryPresenceProbe(repoID))
		if err != nil {
			return edgeVerifyProbeError, "", err
		}
		if !exists {
			return edgeVerifyMissingTarget, repoID, nil
		}
	}
	return edgeVerifyMissingEdge, "", nil
}

func (w *EdgeWriter) recordEdgeVerify(ctx context.Context, outcome string) {
	if w.Instruments == nil || w.Instruments.DeployableUnitEdgeVerify == nil {
		return
	}
	w.Instruments.DeployableUnitEdgeVerify.Add(ctx, 1, metric.WithAttributes(telemetry.AttrOutcome(outcome)))
}

// finishEdgeVerify stamps the verification verdict on the caller's span (the
// reducer's handler span), so a trace shows why a run recorded or skipped its
// reopen fingerprint.
func (w *EdgeWriter) finishEdgeVerify(ctx context.Context, outcome string, present, probed int) {
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String("deployable_unit_edge_verify_outcome", outcome),
		attribute.Int("deployable_unit_edges_verified", present),
		attribute.Int("deployable_unit_edges_probed", probed),
	)
}

func (w *EdgeWriter) warnEdgeVerify(
	outcome string,
	pair [2]string,
	intentID string,
	evidenceSource string,
	present int,
	err error,
) {
	if w.Logger == nil {
		return
	}
	attrs := []any{
		"domain", reducercontract.DomainDeployableUnitEdges,
		"verdict", outcome,
		"source_repo_id", pair[0],
		"target_repo_id", pair[1],
		"sample_intent_id", intentID,
		"evidence_source", evidenceSource,
		"edges_verified_before_miss", present,
	}
	if err != nil {
		attrs = append(attrs, "error", err)
	}
	w.Logger.Warn("deployable unit edge not verified after write, the run records no reopen fingerprint", attrs...)
}
