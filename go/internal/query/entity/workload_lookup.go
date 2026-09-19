// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// workloadLookupCandidateBound caps the name-keyed Workload candidate read.
// It is the shared querycontract.WorkloadSelectorCandidateBound so the
// service-context and deployment-trace selectors fail closed at the same
// size.
const workloadLookupCandidateBound = querycontract.WorkloadSelectorCandidateBound

// lookupWorkloadRow returns the Workload row whereClause selects for the
// caller, or nil. denied reports that at least one row matched its anchor but
// the caller's grant did not admit it; the caller decides whether that
// becomes a grant_denied count, because a later fallback lookup may still
// admit a workload for the same request.
//
// An id-only whereClause reads one row with RunSingle: Workload.id is
// unique-constrained, so that read is exact. Any whereClause with a name
// anchor reads a bounded candidate set instead, because names are not unique
// and an unordered single-row read can return a workload the caller has no
// grant to while a granted one with the same name exists (#6786 review P1).
// The candidates are filtered in Go with querycontract.WorkloadGrantAdmitted
// and the lowest admitted workload id wins, so repeated reads pick the same
// workload. A candidate page over the bound returns
// querycontract.ErrWorkloadSelectorCandidatesExceedBound rather than
// deciding from rows that may be missing a granted workload.
func (h *Handler) lookupWorkloadRow(
	ctx context.Context,
	access querycontract.RepositoryAccessFilter,
	whereClause string,
	params map[string]any,
	selector string,
	operation string,
) (row map[string]any, denied bool, err error) {
	if !strings.Contains(whereClause, "w.name =") {
		row, err = h.Neo4j.RunSingle(ctx, fmt.Sprintf(`
		MATCH (w:Workload) WHERE %s
		RETURN w.id as id, w.name as name, w.kind as kind, w.repo_id as repo_id
		LIMIT 1
	`, whereClause), params)
		if err != nil || row == nil {
			return nil, false, err
		}
		if !workloadRowMatchesAnchor(whereClause, selector, row) {
			h.recordWorkloadAnchorMismatch(ctx, operation, selector, row)
			return nil, false, nil
		}
		return row, false, nil
	}

	// For a scoped caller the SHAPE-A grant predicate joins the candidate
	// WHERE (on the WHERE line, space-led AND) so the bound counts granted
	// rows only: ungranted same-name workloads can neither push a granted
	// caller into the overflow refusal nor signal their own existence to a
	// caller with no grant (#6801 review F-R5-1). The Go re-check below stays
	// as defense in depth against a backend that mis-evaluates the predicate.
	// The caller's clause is parenthesized so the predicate binds to all of
	// it, even if a future caller passes an OR. Past the SHAPE-A inline cap
	// the predicate drops the overflow grants' DEFINES terms and fails closed,
	// so the read emits the #5408 cap signal (#6801 review F-R6-2).
	candidateWhere := whereClause
	if access.Scoped() {
		candidateWhere = "(" + whereClause + ") AND " + querycontract.WorkloadScopePredicate("w", access)
		h.recordScopeGrantInlineCapped(ctx, access, "workload_context_name")
	}
	rows, err := h.readWorkloadCandidates(ctx, candidateWhere, params, operation)
	if err != nil {
		return nil, false, err
	}
	admitted := make([]map[string]any, 0, len(rows))
	var mismatched map[string]any
	for _, candidate := range rows {
		if !workloadRowMatchesAnchor(whereClause, selector, candidate) {
			if mismatched == nil {
				mismatched = candidate
			}
			continue
		}
		if querycontract.StringVal(candidate, "id") == "" {
			continue
		}
		if !querycontract.WorkloadGrantAdmitted(access, querycontract.StringVal(candidate, "repo_id"), querycontract.StringSliceVal(candidate, "defining")) {
			denied = true
			continue
		}
		admitted = append(admitted, candidate)
	}
	if mismatched != nil {
		h.recordWorkloadAnchorMismatch(ctx, operation, selector, mismatched)
	}
	if len(admitted) == 0 {
		return nil, denied, nil
	}
	slices.SortStableFunc(admitted, func(a, b map[string]any) int {
		return strings.Compare(querycontract.StringVal(a, "id"), querycontract.StringVal(b, "id"))
	})
	return admitted[0], denied, nil
}

// readWorkloadCandidates runs the bounded name-keyed candidate read and
// fails closed with querycontract.ErrWorkloadSelectorCandidatesExceedBound
// when the page is over the bound. The overflow logs at Warn with
// reason=candidate_bound_exceeded and the bound, never the row count or the
// selector, so an operator can see the refusal without the log becoming an
// existence oracle for ungranted workloads.
func (h *Handler) readWorkloadCandidates(ctx context.Context, whereClause string, params map[string]any, operation string) ([]map[string]any, error) {
	rows, err := h.Neo4j.Run(ctx, workloadCandidateCypher(whereClause), params)
	if err != nil {
		return nil, err
	}
	if len(rows) > workloadLookupCandidateBound {
		if h.Logger != nil {
			h.Logger.WarnContext(ctx, "workload context name lookup matched more candidates than the bound",
				slog.String("operation", operation),
				slog.Int("candidate_bound", workloadLookupCandidateBound),
				slog.String("reason", "candidate_bound_exceeded"),
			)
		}
		return nil, querycontract.ErrWorkloadSelectorCandidatesExceedBound
	}
	return rows, nil
}

// workloadCandidateCypher renders the bounded name-keyed Workload candidate
// read. It carries the workload's own repo_id and its DEFINES-linked
// repository ids so the grant is decided in Go. whereClause stays on the
// WHERE line and any OR inside it is space-led: a newline- or tab-led AND/OR
// is the shape NornicDB v1.3.3 mis-evaluates (#6786). The DEFINES read is a
// forward OPTIONAL MATCH, not a backward EXISTS subquery.
func workloadCandidateCypher(whereClause string) string {
	return fmt.Sprintf(`
		MATCH (w:Workload) WHERE %s
		OPTIONAL MATCH (dr:Repository)-[:DEFINES]->(w)
		RETURN w.id as id, w.name as name, w.kind as kind, w.repo_id as repo_id, collect(DISTINCT dr.id) as defining
		ORDER BY w.id
		LIMIT %d
	`, whereClause, workloadLookupCandidateBound+1)
}

// workloadRowMatchesAnchor reports whether row satisfies the id and/or name
// anchor whereClause names, compared against selector. It is the #6786 F3
// defense-in-depth guard: a row that matches neither anchored property is
// treated as no row, not trusted because the backend returned it. A clause
// with no recognized anchor admits the row.
func workloadRowMatchesAnchor(whereClause string, selector string, row map[string]any) bool {
	hasIDAnchor := strings.Contains(whereClause, "w.id =")
	hasNameAnchor := strings.Contains(whereClause, "w.name =")
	if !hasIDAnchor && !hasNameAnchor {
		return true
	}
	return (hasIDAnchor && querycontract.StringVal(row, "id") == selector) ||
		(hasNameAnchor && querycontract.StringVal(row, "name") == selector)
}

// recordWorkloadAnchorMismatch logs and counts a backend row that did not
// match its own anchor. This is backend drift, not ordinary authorization,
// so it logs at Warn with reason=backend_anchor_mismatch.
func (h *Handler) recordWorkloadAnchorMismatch(ctx context.Context, operation string, selector string, row map[string]any) {
	if h.Logger != nil {
		h.Logger.WarnContext(ctx,
			"workload context graph row anchor did not match the requested selector",
			"requested_selector", selector,
			"returned_id", querycontract.StringVal(row, "id"),
			"returned_name", querycontract.StringVal(row, "name"),
			"reason", "backend_anchor_mismatch",
		)
	}
	h.recordScopedGrantDenied(ctx, operation, "backend_anchor_mismatch")
}
