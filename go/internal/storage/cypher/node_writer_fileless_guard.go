// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// priorFileCountProbeCypher counts the File nodes a file-less build's
// unbounded retract would delete. It opens on the same anchored shape as
// node.RetractFilesCypher (UNIQUE Repository(id) seek, one REPO_CONTAINS
// expand), so the probe costs at most the retract's own match phase --
// PROFILE on the pinned Neo4j at ~1M File nodes shows ~2M total db hits,
// linear in the repository's own File count and independent of store size
// (evidence-8657-writer-conditional-retract.md). Only the writer issues it,
// at most once per Write, and only for file-less builds.
const priorFileCountProbeCypher = `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
RETURN count(f) AS prior_file_count`

// usesUnboundedFileRetract reports whether buildRetractStatements selects
// the unbounded RetractFilesCypher for mat: a non-first, non-delta,
// repository-scoped materialization carrying zero File rows. It is the
// single predicate both the statement builder and the file-less wipe guard
// read, so the guard can never drift from the shape it protects.
func usesUnboundedFileRetract(mat canonical.CanonicalMaterialization) bool {
	return !mat.FirstGeneration &&
		hasRepositoryScopedRetract(mat) &&
		!mat.DeltaProjection &&
		len(mat.Files) == 0
}

// declaredEmptyScopeForGuard reports whether mat carries the collector's
// genuine-filelessness declaration (the `empty_scope` repository-fact key,
// issue #8668, field landed via #8670). A declared-empty file-less build
// over prior File nodes succeeds; every other file-less wipe over prior
// File nodes fails loud, and the refusal error points at the marker. The
// guard-level test pins the declared-empty path through
// checkFilelessWipeGuard directly.
func declaredEmptyScopeForGuard(mat canonical.CanonicalMaterialization) bool {
	return mat.DeclaredEmptyScope
}

// filelessWipeRefusedError is the loud failure a file-less build over prior
// File nodes returns instead of wiping them. Retryable() is true so the
// projection requeues with backoff instead of dead-lettering: a newer
// generation (or a declared-empty rebuild) resolves the shape, and the
// per-scope supersede fence ends the retry when one arrives. It satisfies
// projector/failure.RetryableError structurally.
type filelessWipeRefusedError struct {
	generationID string
	scopeID      string
	repoID       string
	priorFiles   int64
}

func (e *filelessWipeRefusedError) Error() string {
	return fmt.Sprintf("canonical file-less wipe refused: generation %q scope %q (repository %q) carries zero live and zero tombstoned file facts and zero File rows, but the graph holds %d prior File nodes; refusing a success that would retract them all. If the scope is genuinely fileless, declare it with the empty_scope repository-fact key (issue #8668); tombstone-driven deletes stay exempt",
		e.generationID, e.scopeID, e.repoID, e.priorFiles)
}

// Retryable reports true: the refusal re-enters the durable queue instead
// of becoming terminal on the first failure.
func (e *filelessWipeRefusedError) Retryable() bool { return true }

// filelessWipeGuardOutcome names the guard's decision. Only refused
// carries an error; every other outcome proceeds with the write.
type filelessWipeGuardOutcome string

const (
	// filelessWipeNotApplicable: the build never selects the unbounded
	// file retract, or carries file facts, so no probe runs.
	filelessWipeNotApplicable filelessWipeGuardOutcome = "not_applicable"
	// filelessWipeClear: the probe ran and the graph holds zero prior
	// File nodes, so the retract deletes nothing.
	filelessWipeClear filelessWipeGuardOutcome = "clear"
	// filelessWipeDeclaredEmpty: the #8668 declaration authorizes the
	// wipe, so no probe runs.
	filelessWipeDeclaredEmpty filelessWipeGuardOutcome = "declared_empty"
	// filelessWipeRefused: priors exist and the build fails loud.
	filelessWipeRefused filelessWipeGuardOutcome = "refused_prior_files"
	// filelessWipeProbeUnsupported: no CountProbeExecutor on the chain
	// ("unknown", never "zero rows"), so the retract runs unprobed.
	filelessWipeProbeUnsupported filelessWipeGuardOutcome = "probe_unsupported"
	// filelessWipeProbeFailed: the count probe errored, so the retract
	// runs unprobed rather than failing a projection whose write would
	// succeed.
	filelessWipeProbeFailed filelessWipeGuardOutcome = "probe_failed"
)

// countedFilelessWipeGuardOutcomes are the outcomes that move
// eshu_dp_canonical_fileless_wipe_guard_total. Steady-state projections
// (not_applicable, clear, declared_empty) record nothing, matching the
// repository-retirement precedent.
func (o filelessWipeGuardOutcome) counted() bool {
	return o == filelessWipeRefused ||
		o == filelessWipeProbeUnsupported ||
		o == filelessWipeProbeFailed
}

// checkFilelessWipeGuard probes the prior File count before a file-less
// build may issue the unbounded retract, and fails loud when priors exist.
// declaredEmpty carries the #8668 escape hatch: a declared-empty scope
// skips the probe and authorizes the wipe. Every other exit preserves the
// status-quo behavior: an unprobed backend (no CountProbeExecutor, or a
// probe error, or a negative count) runs the retract exactly as before, so
// a probe-only failure can never fail a projection whose write would
// succeed. Only a definitive prior count above zero refuses.
//
// The probe is advisory with a fail-safe direction (the #6822/#5998
// precedent): found refuses with no write issued, so no TOCTOU harm is
// possible on that path. The residual zero-at-probe to files-at-retract
// window needs a concurrent same-scope file write inside one Write call,
// which the projector queue's one-live-lease-per-scope invariant plus the
// write-start marker fence (#7389/#7819) restrict to the modeled
// stale-attempt class; see evidence-8657-writer-conditional-retract.md.
// No lock, worker, batch, or queue change is added: serialization is not
// the fix.
func checkFilelessWipeGuard(
	ctx context.Context,
	exec Executor,
	mat canonical.CanonicalMaterialization,
	declaredEmpty bool,
) (filelessWipeGuardOutcome, error) {
	if !usesUnboundedFileRetract(mat) {
		return filelessWipeNotApplicable, nil
	}
	if mat.FileIntent.LiveFileFacts > 0 || mat.FileIntent.TombstonedFileFacts > 0 {
		return filelessWipeNotApplicable, nil
	}
	if declaredEmpty {
		return filelessWipeDeclaredEmpty, nil
	}
	prober, ok := exec.(CountProbeExecutor)
	if !ok {
		return filelessWipeProbeUnsupported, nil
	}
	priorFiles, err := prober.ExecuteCountProbe(ctx, Statement{
		Operation:  OperationCanonicalCountProbe,
		Cypher:     priorFileCountProbeCypher,
		Parameters: map[string]any{"repo_id": mat.RepoID},
	})
	if errors.Is(err, ErrCountProbeUnsupported) {
		return filelessWipeProbeUnsupported, nil
	}
	if err != nil || priorFiles < 0 {
		return filelessWipeProbeFailed, nil
	}
	if priorFiles == 0 {
		return filelessWipeClear, nil
	}
	return filelessWipeRefused, &filelessWipeRefusedError{
		generationID: mat.GenerationID,
		scopeID:      mat.ScopeID,
		repoID:       mat.RepoID,
		priorFiles:   priorFiles,
	}
}

// refuseFilelessWipe returns the refusal through the retryable-error
// wrapper. A refusal is already retryable, and WrapRetryableNeo4jError
// passes non-Neo4j errors through unchanged. Span recording lives only in
// recordFilelessWipeGuardOutcome, which already tagged the write span.
func refuseFilelessWipe(_ trace.Span, err error) error {
	return WrapRetryableNeo4jError(err)
}

// runFilelessWipeGuard is the CanonicalNodeWriter.Write entry point: it
// resolves the #8668 declaration seam, runs checkFilelessWipeGuard, and
// owns the counted-outcome telemetry (counter, WARN log, and span) naming
// generation, scope, repository, and prior count. Only the refused outcome
// returns an error; every other outcome returns nil so the write proceeds.
func (w *CanonicalNodeWriter) runFilelessWipeGuard(ctx context.Context, span trace.Span, mat canonical.CanonicalMaterialization) error {
	outcome, err := checkFilelessWipeGuard(ctx, w.executor, mat, declaredEmptyScopeForGuard(mat))
	switch outcome {
	case filelessWipeRefused:
		refused, ok := err.(*filelessWipeRefusedError)
		if !ok || refused == nil {
			return err
		}
		w.recordFilelessWipeGuardOutcome(ctx, span, mat, outcome, refused.priorFiles)
		return refused
	case filelessWipeDeclaredEmpty:
		slog.DebugContext(ctx, "canonical file-less build authorized by declared-empty scope",
			"scope_id", mat.ScopeID,
			"generation_id", mat.GenerationID,
			"repo_id", mat.RepoID,
		)
		return nil
	default:
		if outcome.counted() {
			w.recordFilelessWipeGuardOutcome(ctx, span, mat, outcome, 0)
		}
		return nil
	}
}

// recordFilelessWipeGuardOutcome counts a counted outcome once and logs it
// at WARN with the generation, scope, repository, and (on refusal) prior
// File count. Refusals additionally tag the write span the same way. A nil
// instruments (or an unregistered counter) still logs: the refusal error
// already fails the projection, but a degraded unprobed decision must stay
// visible without metrics.
func (w *CanonicalNodeWriter) recordFilelessWipeGuardOutcome(
	ctx context.Context,
	span trace.Span,
	mat canonical.CanonicalMaterialization,
	outcome filelessWipeGuardOutcome,
	priorFiles int64,
) {
	if w.instruments != nil && w.instruments.CanonicalFilelessWipeGuard != nil {
		w.instruments.CanonicalFilelessWipeGuard.Add(ctx, 1, metric.WithAttributes(
			attribute.String("outcome", string(outcome)),
		))
	}
	if outcome == filelessWipeRefused {
		slog.WarnContext(ctx, "canonical file-less wipe refused: prior File nodes exist",
			"scope_id", mat.ScopeID,
			"generation_id", mat.GenerationID,
			"repo_id", mat.RepoID,
			"prior_file_count", priorFiles,
			"outcome", string(outcome),
		)
		span.SetAttributes(
			telemetry.AttrScopeID(mat.ScopeID),
			telemetry.AttrGenerationID(mat.GenerationID),
			attribute.String("repo_id", mat.RepoID),
			attribute.Int64("prior_file_count", priorFiles),
		)
		span.RecordError(&filelessWipeRefusedError{
			generationID: mat.GenerationID,
			scopeID:      mat.ScopeID,
			repoID:       mat.RepoID,
			priorFiles:   priorFiles,
		})
		span.SetStatus(codes.Error, "file-less wipe refused: prior File nodes exist")
		return
	}
	slog.WarnContext(ctx, "canonical file-less build ran unprobed",
		"scope_id", mat.ScopeID,
		"generation_id", mat.GenerationID,
		"repo_id", mat.RepoID,
		"outcome", string(outcome),
	)
}
