// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	apilibadmin "github.com/eshu-hq/eshu/go/apilib/admin"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The completeness checker itself lives in apilib (#11697) so the external
// API host consumes the same ACTIVE assembly, comparison, and anchored
// Cypher. This file keeps only the core adapter: the Postgres snapshot
// source, the recovery-report conversion that preserves the admission pin
// v0 Check signature, and the core route with its querycontract verdicts.

// DeltaUnionActiveSource reads the ACTIVE side of the completeness check:
// every in-filter scope's active generation plus its union base.
type DeltaUnionActiveSource interface {
	ReadDeltaUnionActive(ctx context.Context, scopeIDs []string) (pgstatus.DeltaUnionActiveSnapshot, error)
}

// DeltaUnionGraphRunner runs one read-only Cypher statement.
type DeltaUnionGraphRunner interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}

// DeltaUnionCompletenessChecker is the read-only prod completeness check
// (#7797). Check delegates to the apilib checker.
type DeltaUnionCompletenessChecker struct {
	// Active reads the ACTIVE side from Postgres.
	Active DeltaUnionActiveSource
	// Graph reads the GRAPH side. Both nil make Check report unconfigured.
	Graph DeltaUnionGraphRunner
	// MaxRepos bounds the repositories one call compares; non-positive
	// selects the default.
	MaxRepos int
}

// ErrDeltaUnionScopeRequired reports a check call without an explicit scope filter.
var ErrDeltaUnionScopeRequired = apilibadmin.ErrDeltaUnionScopeRequired

// deltaUnionActiveAdapter converts the Postgres snapshot to the apilib
// snapshot. Row types are goldengate aliases of the apilib types, so the
// conversion is a direct rewrap with no per-row copy.
type deltaUnionActiveAdapter struct {
	source DeltaUnionActiveSource
}

// ReadDeltaUnionActive reads through the core source and rewraps its snapshot.
func (a deltaUnionActiveAdapter) ReadDeltaUnionActive(ctx context.Context, scopeIDs []string) (apilibadmin.DeltaUnionActiveSnapshot, error) {
	snapshot, err := a.source.ReadDeltaUnionActive(ctx, scopeIDs)
	if err != nil {
		return apilibadmin.DeltaUnionActiveSnapshot{}, err
	}
	return apilibadmin.DeltaUnionActiveSnapshot{Rows: snapshot.Rows, Deltas: snapshot.Deltas}, nil
}

// inner builds the apilib checker behind this core adapter, preserving nil
// ports so the apilib unconfigured verdict applies verbatim.
func (c *DeltaUnionCompletenessChecker) inner() *apilibadmin.DeltaUnionCompletenessChecker {
	if c == nil {
		return nil
	}
	inner := &apilibadmin.DeltaUnionCompletenessChecker{MaxRepos: c.MaxRepos}
	if c.Active != nil {
		inner.Active = deltaUnionActiveAdapter{source: c.Active}
	}
	if c.Graph != nil {
		inner.Graph = c.Graph
	}
	return inner
}

// Check runs the completeness check over an explicit scope filter and
// converts the apilib report to the recovery report the admission pin v0
// promises. Errors pass through with the apilib checker's own wrapping, so
// the route's scope-required, cap, and graph-verdict mapping sees the same
// sentinels as before the extraction.
func (c *DeltaUnionCompletenessChecker) Check(
	ctx context.Context,
	scopeIDs []string,
) (recovery.DeltaUnionCompletenessReport, error) {
	report, err := c.inner().Check(ctx, scopeIDs)
	if err != nil {
		return recovery.DeltaUnionCompletenessReport{}, err
	}
	return recoveryDeltaUnionReport(report), nil
}

// recoveryDeltaUnionReport converts an apilib report to the recovery report
// with identical field values and ordering.
func recoveryDeltaUnionReport(report apilibadmin.DeltaUnionCompletenessReport) recovery.DeltaUnionCompletenessReport {
	out := recovery.DeltaUnionCompletenessReport{
		Repos:      make([]recovery.DeltaUnionRepoReport, 0, len(report.Repos)),
		Mismatches: report.Mismatches,
	}
	for _, repo := range report.Repos {
		out.Repos = append(out.Repos, recovery.DeltaUnionRepoReport{
			RepoID:  repo.RepoID,
			ScopeID: repo.ScopeID,
			Outcome: recovery.DeltaUnionRepoOutcome(repo.Outcome),
			Base: recovery.DeltaUnionGeneration{
				GenerationID: repo.Base.GenerationID, Status: repo.Base.Status, IsDelta: repo.Base.IsDelta,
			},
			Active: recovery.DeltaUnionGeneration{
				GenerationID: repo.Active.GenerationID, Status: repo.Active.Status, IsDelta: repo.Active.IsDelta,
			},
			GraphFiles:  repo.GraphFiles,
			ActiveFiles: repo.ActiveFiles,
			Missing:     repo.Missing,
			Extra:       repo.Extra,
			Drifted:     repo.Drifted,
			Annotation:  repo.Annotation,
		})
	}
	return out
}

// deltaUnionCompleteness serves GET /api/v0/admin/delta-union/completeness:
// the read-only prod completeness report over explicit ?scope= filters. It
// never repairs. A graph verdict answers first (#8292); any other failure
// answers the fixed text through tracing.ServerFailureEnvelope (500, or
// 499 on client cancel). A cap failure keeps its narrow-the-filter 500.
// The body keeps its {"status","detail"} shape.
func (h *Handler) deltaUnionCompleteness(w http.ResponseWriter, r *http.Request) {
	if h.Completeness == nil {
		querycontract.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "unavailable",
			"detail": "The delta-union completeness check is not configured on this API instance.",
		})
		return
	}
	report, err := h.Completeness.Check(r.Context(), r.URL.Query()["scope"])
	if err != nil {
		if errors.Is(err, ErrDeltaUnionScopeRequired) {
			querycontract.WriteJSON(w, http.StatusBadRequest, map[string]any{
				"status": "error",
				"detail": err.Error(),
			})
			return
		}
		if writeDeltaUnionGraphVerdict(w, r, err) {
			return
		}
		message := deltaUnionCompletenessFailedMessage
		var overCap *apilibadmin.DeltaUnionCapError
		if errors.As(err, &overCap) {
			message = overCap.Error()
		}
		status, _ := tracing.ServerFailureEnvelope(r.Context(), err, message, "")
		querycontract.WriteJSON(w, status, map[string]any{
			"status": "error",
			"detail": message,
		})
		return
	}
	querycontract.WriteJSON(w, http.StatusOK, report)
}

// writeDeltaUnionGraphVerdict answers err with the shared graph-read verdict
// under deltaUnionCompletenessCapability and reports whether it did (#8292).
func writeDeltaUnionGraphVerdict(w http.ResponseWriter, r *http.Request, err error) bool {
	status, envelope, ok := querycontract.GraphReadErrorEnvelope(err, deltaUnionCompletenessCapability)
	if !ok {
		return false
	}
	if querycontract.AcceptsEnvelope(r) {
		querycontract.WriteErrorEnvelope(w, r, status, envelope)
		return true
	}
	if envelope.Retryable() {
		w.Header().Set("Retry-After", strconv.Itoa(querycontract.BackendUnavailableRetryAfterSeconds))
	}
	querycontract.WriteJSON(w, status, map[string]any{
		"status": "error",
		"detail": envelope.Message,
	})
	return true
}
