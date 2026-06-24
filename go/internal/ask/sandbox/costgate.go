// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sandbox

// costgate.go — pre-execution SQL query-plan cost check for the ask/sandbox.
//
// # Design
//
// The cost gate is a sandbox-execution-layer concern (Layer 3.5). It runs
// EXPLAIN (FORMAT JSON) inside the SAME read-only transaction used for query
// execution so that per-tx state (RLS SET LOCAL, search_path, statement
// timeout) applies to both the plan check and the real query.
//
// Design decision (issue #3302): the cost gate lives in the sandbox executor,
// not the API layer. Because the EXPLAIN and the query MUST share one
// transaction to validate the same plan that will run, the check is
// co-located with the read-only-tx executor (pgexec.go). This is a
// defense-in-depth plan-cost check for LLM-authored ad-hoc queries.
//
// # Ownership
//
// Tenant scope-predicate injection (ensuring queries only return the
// authenticated tenant's rows) remains the responsibility of the API layer
// (issue #3263). That is the open design question tracked in the #3302 design
// package; it is NOT solved here.
//
// # Unit-testability
//
// The plan-parsing and budget-check logic (parsePlanSummary, findForbiddenOperator,
// CheckPlan) is decoupled from the tx orchestration. CostGateExecutor.Exec
// detects whether its inner Executor supports in-tx plan checking (via the
// inTxPlanChecker interface implemented by postgresReadOnlyExecutor). When it
// does, the orchestration runs both EXPLAIN and query in one tx inside the
// inner executor. When it does not (e.g. a mock inner in unit tests),
// CostGateExecutor falls back to the SQLExplainer-based path so that the
// 13 cost-gate unit tests remain valid without a live database.
//
// # Conservative behaviour
//
//   - If the EXPLAIN call fails (backend unavailable, syntax error) the query is
//     rejected with "cost gate: plan check failed" — never silently allowed.
//   - Forbidden plan operators (Seq Scan) are checked when the operator list is
//     not empty. v1 ships with an empty forbidden list; callers can extend via
//     the CostGateConfig.ForbiddenPlanOperators field.
//   - Deny reasons are bounded: they never echo the query or reveal schema names.
//   - The cost gate is a no-op when all limits are zero and no forbidden
//     operators are configured.
//
// The cost gate vocabulary (Total Cost, Plan Rows, operator names) mirrors the
// PlanExpectation type in go/internal/queryplan, which is the authoritative gate
// for hot-path queries. The sandbox cost gate is the runtime enforcement
// counterpart for LLM-authored ad-hoc queries.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// CostGateConfig controls which plan checks the cost gate applies in addition
// to the Caps-level MaxPlanCost and MaxEstimatedRows limits.
//
// The zero value applies no forbidden-operator checks and is safe to use.
type CostGateConfig struct {
	// ForbiddenPlanOperators is the set of Postgres plan node types that are
	// never permitted in sandboxed queries. Node type names are matched
	// case-insensitively against the "Node Type" field in EXPLAIN JSON output.
	// An empty slice (the default) applies no operator restriction.
	//
	// Example: []string{"Seq Scan"} rejects any plan containing a sequential
	// scan, forcing the planner to use an index path.
	ForbiddenPlanOperators []string
}

// PlanSummary holds the planner estimates extracted from EXPLAIN (FORMAT JSON)
// output. It is returned by CheckPlan so callers can log the values without
// needing to re-parse the raw JSON.
type PlanSummary struct {
	// TotalCost is the planner's total cost estimate for the root plan node.
	TotalCost float64
	// EstimatedRows is the planner's row-count estimate for the root plan node.
	EstimatedRows float64
	// ForbiddenOperator is the first forbidden operator found in the plan, or
	// empty if no forbidden operators were detected.
	ForbiddenOperator string
}

// inTxPlanChecker is implemented by Executor types that can run the plan check
// and query execution in the same database transaction. CostGateExecutor.Exec
// probes for this interface on the inner Executor to determine whether to route
// through the in-tx path (production) or the SQLExplainer path (tests).
type inTxPlanChecker interface {
	execWithPlanCheck(ctx context.Context, query string, caps Caps, cfg CostGateConfig) (int, error)
}

// CostGateExecutor wraps an inner Executor and enforces a pre-execution cost
// check for SQL queries via EXPLAIN (FORMAT JSON). It implements Executor.
//
// When inner implements inTxPlanChecker (e.g. postgresReadOnlyExecutor), the
// EXPLAIN and the actual query run in the SAME read-only transaction so that
// per-session state (RLS, search_path, statement timeout) is identical for both.
// This is the production path.
//
// When inner does not implement inTxPlanChecker (e.g. a mock Executor in unit
// tests), CostGateExecutor falls back to calling g.explainer for EXPLAIN JSON
// and then inner.Exec for execution. This path is for unit tests only.
//
// Construct via NewCostGateExecutor.
type CostGateExecutor struct {
	inner     Executor
	explainer SQLExplainer
	cfg       CostGateConfig
}

// SQLExplainer runs EXPLAIN (FORMAT JSON) for a SQL query and returns the raw
// JSON bytes. The interface exists so tests can inject a mock without a live
// database. In production the EXPLAIN is run inside the same read-only tx as
// query execution by postgresReadOnlyExecutor (the inTxPlanChecker path).
//
// The context carries the caller deadline and must be respected.
type SQLExplainer interface {
	// Explain returns the raw EXPLAIN (FORMAT JSON) output for query, or an
	// error if the plan cannot be obtained.
	Explain(ctx context.Context, query string) ([]byte, error)
}

// NewCostGateExecutor wraps inner with a pre-execution cost gate. cfg controls
// optional forbidden-operator checks.
//
// If inner implements inTxPlanChecker (e.g. postgresReadOnlyExecutor created
// via NewPostgresReadOnlyExecutorWithCostGate), the cost gate runs inside the
// inner executor's read-only transaction. The explainer argument is used only
// for CheckPlan (observability) and as the fallback for non-inTxPlanChecker
// inner executors (unit-test path).
//
// If both Caps.MaxPlanCost and Caps.MaxEstimatedRows are zero AND cfg has no
// ForbiddenPlanOperators, the cost gate is a pass-through regardless of path.
func NewCostGateExecutor(inner Executor, explainer SQLExplainer, cfg CostGateConfig) *CostGateExecutor {
	return &CostGateExecutor{inner: inner, explainer: explainer, cfg: cfg}
}

// Exec checks the SQL query plan before delegating to the inner Executor.
//
// For DialectCypher, Exec delegates directly to inner.Exec — Cypher cost
// gating is not wired in v1.
//
// For DialectSQL, Exec:
//  1. If MaxPlanCost == 0 AND MaxEstimatedRows == 0 AND no ForbiddenPlanOperators
//     are configured, skip the cost check and call inner.Exec directly.
//  2. If inner implements inTxPlanChecker, delegate entirely to
//     inner.execWithPlanCheck so that EXPLAIN and execution share one
//     read-only transaction, both bounded by caps.Timeout. This is the
//     production path when inner is a postgresReadOnlyExecutor.
//  3. Otherwise (unit-test / mock path): call CheckPlan via the SQLExplainer,
//     reject on over-budget or forbidden operator, then call inner.Exec.
func (g *CostGateExecutor) Exec(ctx context.Context, dialect Dialect, query string, caps Caps) (int, error) {
	if dialect != DialectSQL {
		return g.inner.Exec(ctx, dialect, query, caps)
	}

	// Skip the cost gate when all limits are zero and no operator checks are
	// configured — this keeps the code path cheap for callers that want the
	// cost gate present but have not yet configured limits.
	if caps.MaxPlanCost == 0 && caps.MaxEstimatedRows == 0 && len(g.cfg.ForbiddenPlanOperators) == 0 {
		return g.inner.Exec(ctx, dialect, query, caps)
	}

	// Production path: inner executor supports in-tx plan checking. The EXPLAIN
	// and the query run in the SAME read-only Postgres transaction, bounded by
	// caps.Timeout. This ensures per-session state (RLS SET LOCAL, search_path,
	// statement_timeout) is identical for both the plan check and execution.
	if checker, ok := g.inner.(inTxPlanChecker); ok {
		return checker.execWithPlanCheck(ctx, query, caps, g.cfg)
	}

	// Fallback path (unit tests / mock executors): use the injected SQLExplainer
	// for the plan check, then call inner.Exec separately.
	summary, err := g.CheckPlan(ctx, query)
	if err != nil {
		return 0, err
	}

	if summary.ForbiddenOperator != "" {
		return 0, fmt.Errorf("%w: forbidden plan operator %s", ErrPlanBudgetExceeded, summary.ForbiddenOperator)
	}
	if caps.MaxPlanCost > 0 && summary.TotalCost > caps.MaxPlanCost {
		return 0, fmt.Errorf("%w: total cost %.2f exceeds budget %.2f",
			ErrPlanBudgetExceeded, summary.TotalCost, caps.MaxPlanCost)
	}
	if caps.MaxEstimatedRows > 0 && summary.EstimatedRows > caps.MaxEstimatedRows {
		return 0, fmt.Errorf("%w: estimated rows %.0f exceeds budget %.0f",
			ErrPlanBudgetExceeded, summary.EstimatedRows, caps.MaxEstimatedRows)
	}

	return g.inner.Exec(ctx, dialect, query, caps)
}

// CheckPlan runs EXPLAIN (FORMAT JSON) and returns a PlanSummary. It is
// exported so callers can log or surface the plan estimates for observability.
//
// CheckPlan uses the SQLExplainer (not an in-tx call) and is intended for
// operator-facing observability, not for the production execution path. The
// production in-tx plan check runs inside execWithPlanCheck on
// postgresReadOnlyExecutor.
//
// An error is returned if the explainer fails or the JSON cannot be parsed.
// CheckPlan never returns a nil error with a partially-filled summary; on any
// error the returned summary is the zero value.
func (g *CostGateExecutor) CheckPlan(ctx context.Context, query string) (PlanSummary, error) {
	raw, err := g.explainer.Explain(ctx, query)
	if err != nil {
		return PlanSummary{}, fmt.Errorf("cost gate: plan check failed: %w", err)
	}
	return parsePlanSummary(raw, g.cfg.ForbiddenPlanOperators)
}

// ErrPlanBudgetExceeded is returned by CostGateExecutor.Exec when a query plan
// exceeds the configured cost or row budget, or contains a forbidden operator.
// The caller should treat this as a bounded, leak-safe denial: the error message
// is safe to return to the model for reformulation but must not be forwarded
// verbatim to end users without review.
var ErrPlanBudgetExceeded = errors.New("query plan exceeds cost budget")

// explainNode is a partial decode target for one node in EXPLAIN FORMAT JSON
// output. Postgres nests child nodes under "Plans"; we walk the tree to find
// forbidden operators.
type explainNode struct {
	NodeType  string        `json:"Node Type"`
	TotalCost float64       `json:"Total Cost"`
	PlanRows  float64       `json:"Plan Rows"`
	Plans     []explainNode `json:"Plans"`
}

// explainRoot is the top-level structure of EXPLAIN (FORMAT JSON) output.
// Postgres returns an array of plan objects, each with a "Plan" key.
type explainRoot struct {
	Plan explainNode `json:"Plan"`
}

// parsePlanSummary decodes raw EXPLAIN (FORMAT JSON) bytes and returns a
// PlanSummary. forbidden is the list of node type strings to reject.
// Called by both CostGateExecutor.CheckPlan and postgresReadOnlyExecutor.execWithPlanCheck.
func parsePlanSummary(raw []byte, forbidden []string) (PlanSummary, error) {
	var roots []explainRoot
	if err := json.Unmarshal(raw, &roots); err != nil {
		return PlanSummary{}, fmt.Errorf("cost gate: cannot parse EXPLAIN JSON: %w", err)
	}
	if len(roots) == 0 {
		return PlanSummary{}, errors.New("cost gate: EXPLAIN returned empty plan")
	}

	root := roots[0].Plan
	summary := PlanSummary{
		TotalCost:     root.TotalCost,
		EstimatedRows: root.PlanRows,
	}

	if len(forbidden) > 0 {
		if op := findForbiddenOperator(root, forbidden); op != "" {
			summary.ForbiddenOperator = op
		}
	}

	return summary, nil
}

// findForbiddenOperator walks the plan tree and returns the first node type
// that matches a forbidden operator name (case-insensitive), or empty string.
func findForbiddenOperator(node explainNode, forbidden []string) string {
	upper := strings.ToUpper(node.NodeType)
	for _, f := range forbidden {
		if strings.ToUpper(f) == upper {
			return node.NodeType
		}
	}
	for _, child := range node.Plans {
		if op := findForbiddenOperator(child, forbidden); op != "" {
			return op
		}
	}
	return ""
}
