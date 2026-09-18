// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
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
func ResolveTraceWorkloadSelector(ctx context.Context, reader querycontract.GraphQuery, selector string) (string, error) {
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
	if idRow != nil && querycontract.WorkloadGrantAdmitted(access, querycontract.StringVal(idRow, "repo_id"), querycontract.StringSliceVal(idRow, "defining")) {
		return querycontract.StringVal(idRow, "id"), nil
	}

	nameRows, err := reader.Run(ctx, fmt.Sprintf("%s\nLIMIT %d", workloadSelectorRowCypher("w.name = $service_name"), traceWorkloadSelectorCandidateBound+1), params)
	if err != nil {
		return "", err
	}
	nameAdmitted, err := admittedWorkloadCandidates(access, nameRows)
	if err != nil {
		return "", err
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
// workloadSelectorRowCypher read) to those access's grant admits, in the same
// order. It fails closed with errTraceWorkloadSelectorCandidatesExceedBound
// when rows reached the fetch bound, rather than deciding admission/ambiguity
// from a page that may be missing granted rows past the bound.
func admittedWorkloadCandidates(access querycontract.RepositoryAccessFilter, rows []map[string]any) ([]map[string]any, error) {
	if len(rows) > traceWorkloadSelectorCandidateBound {
		return nil, fmt.Errorf("%w: %d", errTraceWorkloadSelectorCandidatesExceedBound, len(rows))
	}
	admitted := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		repoID := querycontract.StringVal(row, "repo_id")
		definingRepoIDs := querycontract.StringSliceVal(row, "defining")
		if querycontract.WorkloadGrantAdmitted(access, repoID, definingRepoIDs) {
			admitted = append(admitted, row)
		}
	}
	return admitted, nil
}
