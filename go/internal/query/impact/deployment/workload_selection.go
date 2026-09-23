// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

var errAmbiguousWorkloadSelector = errors.New("deployment trace workload selector is ambiguous")

// ErrAmbiguousWorkloadSelector is the exported form of
// errAmbiguousWorkloadSelector: the impact package matches it with
// errors.Is from outside this package. See #6060.
var ErrAmbiguousWorkloadSelector = errAmbiguousWorkloadSelector

// workloadSelectorCandidateBound caps how many Workload rows the name lookup
// reads before deciding admission and ambiguity in Go. It is the shared
// querycontract.WorkloadSelectorCandidateBound; see that constant for why a
// page that reaches the bound fails closed with
// querycontract.ErrWorkloadSelectorCandidatesExceedBound.
//
// The retired implementation compared only the first two name-matching rows
// (SKIP 1 LIMIT 1). That was safe only while the Cypher WHERE filtered to the
// caller's granted rows. With the grant decided in Go, the first raw rows are
// no longer guaranteed granted, so every row up to the bound is inspected.
const workloadSelectorCandidateBound = querycontract.WorkloadSelectorCandidateBound

// ResolveWorkloadSelector resolves selector (a Workload id or name) to the
// caller's granted Workload id for the deployment-trace impact routes, or
// ("", nil) when nothing admitted matches or the caller has no grant at all.
//
// Workload.id is a unique-constrained property, so the id lookup reads at
// most one row and RunSingle is exact. Workload names are not unique, so the
// name lookup reads a bounded batch. For a scoped caller that read carries
// querycontract.WorkloadScopePredicate on its WHERE line, so the bound counts
// granted rows only. Go then decides admission and ambiguity over the batch
// (admittedWorkloadCandidates), re-checking every row. Ambiguity counts distinct admitted
// workload ids, so duplicate rows for one workload never hide or fake a
// second one.
//
// Errors: a name match on two or more distinct admitted ids wraps
// ErrAmbiguousWorkloadSelector; a name page over the bound returns
// querycontract.ErrWorkloadSelectorCandidatesExceedBound unwrapped, so its
// fixed text never carries a row count.
//
// logger and instruments are nil-tolerant. A row whose own id or name
// disagrees with the selector logs a `reason=backend_anchor_mismatch` Warn and
// counts telemetry.Instruments.QueryScopedGrantDenied with that reason. An
// ordinary scoped denial counts `reason=grant_denied`, once per request, and
// only when neither lookup admitted a workload: an id-lookup denial followed
// by a name-lookup admit is a successful request, not a denial.
func ResolveWorkloadSelector(
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
	params := access.GraphParams(map[string]any{"service_name": selector})
	warnMismatch := func(stage string) {
		if logger != nil {
			logger.WarnContext(ctx, "deployment trace selector "+stage+" row did not match the requested selector",
				slog.String("operation", operation), slog.String("reason", "backend_anchor_mismatch"))
		}
		recordScopedGrantDenied(ctx, instruments, operation, "backend_anchor_mismatch")
	}

	idRow, err := reader.RunSingle(ctx, workloadSelectorRowCypher("w.id = $service_name")+"\nLIMIT 1", params)
	if err != nil {
		return "", err
	}
	// #6786 review follow-up (F3): require the returned row's own id to equal
	// the selector, the same defense-in-depth guard GetEntityContext
	// (entity/handler.go) applies. A backend that ever regresses the anchor
	// must not fall through to a different workload's data.
	denied := false
	if idRow != nil {
		gotID := querycontract.StringVal(idRow, "id")
		switch {
		case gotID != selector:
			warnMismatch("id-lookup")
		case querycontract.WorkloadGrantAdmitted(access, querycontract.StringVal(idRow, "repo_id"), querycontract.StringSliceVal(idRow, "defining")):
			return gotID, nil
		default:
			denied = true
		}
	}

	// A scoped caller's name read carries the SHAPE-A grant predicate on the
	// WHERE line so the candidate bound counts granted rows only (#6801
	// review F-R5-1); admittedWorkloadCandidates still re-checks every row.
	// Past the SHAPE-A inline cap the predicate drops the overflow grants'
	// DEFINES terms and fails closed, so the read emits the #5408 cap signal
	// (#6801 review F-R6-2).
	nameWhere := "w.name = $service_name"
	if access.Scoped() {
		nameWhere += " AND " + querycontract.WorkloadScopePredicate("w", access)
		recordScopeGrantInlineCapped(ctx, logger, instruments, access, "deployment_trace_selector")
	}
	nameRows, err := reader.Run(ctx, fmt.Sprintf("%s\nLIMIT %d", workloadSelectorRowCypher(nameWhere), workloadSelectorCandidateBound+1), params)
	if err != nil {
		return "", err
	}
	admittedIDs, nameMismatched, nameDenied, err := admittedWorkloadCandidates(access, selector, nameRows)
	if err != nil {
		return "", err
	}
	if nameMismatched {
		warnMismatch("name-lookup")
	}
	switch len(admittedIDs) {
	case 0:
		if denied || nameDenied {
			recordScopedGrantDenied(ctx, instruments, operation, "grant_denied")
		}
		return "", nil
	case 1:
		return admittedIDs[0], nil
	default:
		return "", fmt.Errorf("%w: %q matched at least two workload ids", errAmbiguousWorkloadSelector, selector)
	}
}

// workloadSelectorRowCypher renders the shared MATCH/RETURN/ORDER BY shell
// for a Workload selector lookup keyed by whereClause, carrying the
// workload's materialized repo_id and its DEFINES-linked repository ids so
// the caller can decide grant admission in Go.
//
// This function adds no grant of its own. For a scoped caller,
// ResolveWorkloadSelector appends the single-line SHAPE-A
// querycontract.WorkloadScopePredicate to the name lookup's whereClause
// before calling it. It never renders a multi-line `AND ( ... OR EXISTS {...} )`
// group, which is unreliable on the pinned NornicDB v1.3.3 image and can drop
// the whole WHERE, including whereClause's own id/name anchor (#6786).
// admittedWorkloadCandidates / querycontract.WorkloadGrantAdmitted re-check
// every row in Go.
func workloadSelectorRowCypher(whereClause string) string {
	return fmt.Sprintf(`
		MATCH (w:Workload) WHERE %s
		OPTIONAL MATCH (dr:Repository)-[:DEFINES]->(w)
		RETURN w.id as id, w.name as name, w.repo_id as repo_id, collect(DISTINCT dr.id) as defining
		ORDER BY w.id`, whereClause)
}

// admittedWorkloadCandidates filters rows from a workloadSelectorRowCypher
// name lookup to the distinct, sorted workload ids whose row name equals
// selector and whose grant access admits.
//
// It also reports nameMismatch when a row's own name disagreed with selector
// (the F3 backend-anchor-mismatch signal) and denied when at least one
// name-matched row was not admitted. It returns
// querycontract.ErrWorkloadSelectorCandidatesExceedBound when rows exceed the
// bound, rather than deciding from a page that may be missing granted rows.
func admittedWorkloadCandidates(
	access querycontract.RepositoryAccessFilter,
	selector string,
	rows []map[string]any,
) (admittedIDs []string, nameMismatch bool, denied bool, err error) {
	if len(rows) > workloadSelectorCandidateBound {
		return nil, false, false, querycontract.ErrWorkloadSelectorCandidatesExceedBound
	}
	for _, row := range rows {
		if querycontract.StringVal(row, "name") != selector {
			nameMismatch = true
			continue
		}
		id := querycontract.StringVal(row, "id")
		if id == "" {
			continue
		}
		if !querycontract.WorkloadGrantAdmitted(access, querycontract.StringVal(row, "repo_id"), querycontract.StringSliceVal(row, "defining")) {
			denied = true
			continue
		}
		if !slices.Contains(admittedIDs, id) {
			admittedIDs = append(admittedIDs, id)
		}
	}
	slices.Sort(admittedIDs)
	return admittedIDs, nameMismatch, denied, nil
}
