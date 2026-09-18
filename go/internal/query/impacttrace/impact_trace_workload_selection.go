// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

var errAmbiguousTraceWorkloadSelector = errors.New("deployment trace workload selector is ambiguous")

// ErrAmbiguousTraceWorkloadSelector is the exported form of
// errAmbiguousTraceWorkloadSelector: the impact package matches it with
// errors.Is from outside this package. See #6060.
var ErrAmbiguousTraceWorkloadSelector = errAmbiguousTraceWorkloadSelector

// errTraceWorkloadSelectorCandidatesExceedBound is returned when a name-based
// selector lookup finds more candidate rows than
// traceWorkloadSelectorCandidateBound. See that constant's doc comment for
// why this fails closed rather than silently working from a truncated page.
var errTraceWorkloadSelectorCandidatesExceedBound = errors.New("deployment trace workload selector candidates exceed bound")

// traceWorkloadSelectorCandidateBound caps how many Workload rows a
// name-based selector lookup reads before deciding admission and ambiguity in
// Go. It mirrors the order of magnitude FetchWorkloadRepositoryForAccess
// (entity/workload_context.go) uses for its own DEFINES candidate bound.
//
// The bound matters because the grant is now decided in Go rather than in the
// Cypher WHERE (see ResolveTraceWorkloadSelector's doc comment): the retired
// implementation compared only the first two name-matching rows (SKIP 1
// LIMIT 1), which was safe only because the Cypher WHERE had already
// filtered to the caller's granted rows -- position 1 and 2 were guaranteed
// to both be granted. Deciding the grant in Go instead means those first two
// raw rows are no longer guaranteed granted, so a genuine granted duplicate
// could sit past position 2 behind ungranted rows and be missed by a plain
// two-row compare. Reading up to this bound and filtering by grant in Go
// fixes that, but only up to the bound itself -- if the raw (pre-grant-
// filter) row count reaches it, more candidates may exist beyond what was
// read, and admission/ambiguity decided from a truncated page could silently
// drop a granted duplicate that would have made the selector ambiguous.
// ResolveTraceWorkloadSelector reports
// errTraceWorkloadSelectorCandidatesExceedBound in that case rather than
// guessing.
const traceWorkloadSelectorCandidateBound = 50

// ResolveTraceWorkloadSelector resolves selector (a Workload id or name) to
// the caller's granted Workload id for the deployment-trace impact route, or
// ("", nil) when nothing matches or the caller has no grant at all.
//
// Workload.id is a unique-constrained property, so the id-lookup stage reads
// at most one row and RunSingle is exact. Workload names are not unique, so
// the name-lookup stage reads a bounded batch (workloadCandidates) and
// decides admission and ambiguity over it in Go
// (admittedWorkloadCandidates) -- see that function's doc comment for why a
// plain first/second-row compare is not safe once the grant is no longer
// enforced by the Cypher WHERE.
//
// logger and instruments are the #6786 review follow-up (R2-4) operator-
// visibility seam: both are nil-tolerant (most callers, including every
// non-live test in this package, pass nil for either or both) so emission is
// simply skipped when unwired. When set, a row that fails the F3 row-identity
// guard below logs a `reason=backend_anchor_mismatch` Warn and increments
// telemetry.Instruments.QueryScopedGrantDenied; an ordinary scoped denial
// (the caller has no grant to the resolved workload at all) increments the
// same counter with `reason=grant_denied`. See
// go/internal/query/entity/scoped_grant_telemetry.go for the sibling
// emission seam this mirrors.
func ResolveTraceWorkloadSelector(
	ctx context.Context,
	reader querycontract.GraphQuery,
	selector string,
	logger *slog.Logger,
	instruments *telemetry.Instruments,
) (string, error) {
	const operation = "deployment_trace_selector"
	selector = strings.TrimSpace(selector)
	if reader == nil || selector == "" {
		return "", nil
	}
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	if access.Empty() {
		return "", nil
	}
	params := map[string]any{"service_name": selector}

	idRow, err := reader.RunSingle(ctx, workloadSelectorRowCypher("w.id = $service_name")+"\nLIMIT 1", params)
	if err != nil {
		return "", err
	}
	// #6786 review follow-up (F3): require the returned row's own id to equal
	// the selector, the same defense-in-depth guard GetEntityContext
	// (entity/handler.go) applies. workloadSelectorRowCypher's WHERE is a
	// single-line `w.id = $service_name` with no embedded grant predicate, so
	// this anchor is not the multi-line shape #6786 proved NornicDB v1.3.3
	// can drop -- but a backend that ever regresses that anchor must not go
	// unnoticed by falling through to a DIFFERENT in-grant workload's data.
	if idRow != nil {
		gotID := querycontract.StringVal(idRow, "id")
		if gotID != selector {
			if logger != nil {
				logger.WarnContext(ctx, "deployment trace selector id-lookup row did not match the requested selector",
					slog.String("operation", operation), slog.String("reason", "backend_anchor_mismatch"))
			}
			recordScopedGrantDenied(ctx, instruments, operation, "backend_anchor_mismatch")
		} else if querycontract.WorkloadGrantAdmitted(access, querycontract.StringVal(idRow, "repo_id"), querycontract.StringSliceVal(idRow, "defining")) {
			return gotID, nil
		} else {
			recordScopedGrantDenied(ctx, instruments, operation, "grant_denied")
		}
	}

	nameRows, err := reader.Run(ctx, fmt.Sprintf("%s\nLIMIT %d", workloadSelectorRowCypher("w.name = $service_name"), traceWorkloadSelectorCandidateBound+1), params)
	if err != nil {
		return "", err
	}
	nameAdmitted, nameMismatched, err := admittedWorkloadCandidates(access, selector, nameRows)
	if err != nil {
		return "", err
	}
	switch {
	case nameMismatched:
		if logger != nil {
			logger.WarnContext(ctx, "deployment trace selector name-lookup row did not match the requested selector",
				slog.String("operation", operation), slog.String("reason", "backend_anchor_mismatch"))
		}
		recordScopedGrantDenied(ctx, instruments, operation, "backend_anchor_mismatch")
	case len(nameAdmitted) == 0 && len(nameRows) > 0:
		// Every row's own name matched the selector (no anchor-mismatch
		// signal), but none was grant-admitted: an ordinary scoped denial,
		// not a backend regression.
		recordScopedGrantDenied(ctx, instruments, operation, "grant_denied")
	}
	if len(nameAdmitted) == 0 {
		return "", nil
	}
	if len(nameAdmitted) > 1 && querycontract.StringVal(nameAdmitted[0], "id") != querycontract.StringVal(nameAdmitted[1], "id") {
		return "", fmt.Errorf("%w: %q matched at least two workload ids", errAmbiguousTraceWorkloadSelector, selector)
	}
	return querycontract.StringVal(nameAdmitted[0], "id"), nil
}

// workloadSelectorRowCypher renders the shared MATCH/RETURN/ORDER BY shell
// for a Workload selector lookup keyed by whereClause, carrying the
// workload's materialized repo_id and its DEFINES-linked repository ids so
// the caller can decide grant admission in Go.
//
// The predicate is unconditional Cypher -- no scoped grant is appended here.
// This selector and GetEntityContext (entity/handler.go) both used to render
// a scoped grant as a multi-line `AND ( ... OR EXISTS {...} )` WHERE group,
// which is unreliable on the pinned NornicDB v1.3.3 image: it can silently
// drop the WHOLE WHERE, including whereClause's own id/name anchor, so a
// scoped caller's selector could resolve to an unrelated, ungranted Workload
// (#6786). The grant is decided by admittedWorkloadCandidates /
// querycontract.WorkloadGrantAdmitted instead, from rows this Cypher fetches
// unfiltered.
func workloadSelectorRowCypher(whereClause string) string {
	return fmt.Sprintf(`
		MATCH (w:Workload) WHERE %s
		OPTIONAL MATCH (dr:Repository)-[:DEFINES]->(w)
		RETURN w.id as id, w.name as name, w.repo_id as repo_id, collect(DISTINCT dr.id) as defining
		ORDER BY w.id`, whereClause)
}

// admittedWorkloadCandidates filters rows (as returned by a
// workloadSelectorRowCypher name-lookup read) to those whose own `name`
// equals selector AND access's grant admits, in the same order. The name
// check is a defense-in-depth row-equality guard (#6786 review follow-up,
// F3): workloadSelectorRowCypher's WHERE is a single-line
// `w.name = $service_name` with no embedded grant predicate, so it is not
// the multi-line shape NornicDB v1.3.3 was proven to drop, but a backend
// that ever regressed that anchor must not silently hand back a different,
// merely-admitted workload's data. It fails closed with
// errTraceWorkloadSelectorCandidatesExceedBound when rows reached the fetch
// bound, rather than deciding admission/ambiguity from a page that may be
// missing granted rows past the bound.
// admittedWorkloadCandidates also reports nameMismatch (#6786 review
// follow-up, R2-4): true when at least one row's own `name` disagreed with
// selector, the same F3 backend-anchor-mismatch signal the id-lookup stage
// reports. ResolveTraceWorkloadSelector logs and counts it as
// `backend_anchor_mismatch`; a name-matched row that the grant simply does
// not admit is counted as an ordinary `grant_denied` instead.
func admittedWorkloadCandidates(access querycontract.RepositoryAccessFilter, selector string, rows []map[string]any) (admitted []map[string]any, nameMismatch bool, err error) {
	if len(rows) > traceWorkloadSelectorCandidateBound {
		return nil, false, fmt.Errorf("%w: %d", errTraceWorkloadSelectorCandidatesExceedBound, len(rows))
	}
	admitted = make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if querycontract.StringVal(row, "name") != selector {
			nameMismatch = true
			continue
		}
		repoID := querycontract.StringVal(row, "repo_id")
		definingRepoIDs := querycontract.StringSliceVal(row, "defining")
		if querycontract.WorkloadGrantAdmitted(access, repoID, definingRepoIDs) {
			admitted = append(admitted, row)
		}
	}
	return admitted, nameMismatch, nil
}
