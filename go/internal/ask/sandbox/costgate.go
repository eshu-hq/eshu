package sandbox

// costgate.go — pre-execution SQL query-plan cost check for the ask/sandbox.
//
// The cost gate runs EXPLAIN (FORMAT JSON) inside the same read-only transaction
// used for query execution. It parses the root node's Total Cost and Plan Rows
// from the returned JSON and rejects the query if either value exceeds the
// configured caps before the real query is sent to the executor.
//
// This is Layer 3.5: it executes only when the Guard is enabled (Layer 1 and
// Layer 2 have already passed) and only for DialectSQL (Cypher is not wired in
// v1). If Caps.MaxPlanCost and Caps.MaxEstimatedRows are both zero the cost
// gate is a no-op and the query proceeds directly to the executor.
//
// The cost gate is intentionally conservative:
//   - If the EXPLAIN call fails (backend unavailable, syntax error) the query is
//     rejected with "cost gate: plan check failed" — never silently allowed.
//   - Forbidden plan operators (Seq Scan) are checked when the operator list is
//     not empty. v1 ships with an empty forbidden list; callers can extend via
//     the CostGateConfig.ForbiddenPlanOperators field.
//   - Deny reasons are bounded: they never echo the query or reveal schema names.
//
// Design alignment: the cost gate vocabulary (Total Cost, Plan Rows, operator
// names) mirrors the PlanExpectation type in go/internal/queryplan, which is
// the authoritative gate for hot-path queries. The sandbox cost gate is the
// runtime enforcement counterpart for LLM-authored ad-hoc queries.

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

// CostGateExecutor wraps an inner Executor and enforces a pre-execution cost
// check for SQL queries via EXPLAIN (FORMAT JSON). It implements Executor.
//
// Construct via NewCostGateExecutor.
type CostGateExecutor struct {
	inner     Executor
	explainer SQLExplainer
	cfg       CostGateConfig
}

// SQLExplainer runs EXPLAIN (FORMAT JSON) for a SQL query and returns the raw
// JSON bytes. Implementations must operate inside a read-only context; the cost
// gate calls this before the real query runs.
//
// The interface exists so tests can inject a mock without a live database.
type SQLExplainer interface {
	// Explain returns the raw EXPLAIN (FORMAT JSON) output for query, or an
	// error if the plan cannot be obtained. The context carries the caller
	// deadline and must be respected.
	Explain(ctx context.Context, query string) ([]byte, error)
}

// NewCostGateExecutor wraps inner with a pre-execution cost gate that uses
// explainer to obtain EXPLAIN JSON for SQL queries before forwarding them to
// inner.Exec. cfg controls optional forbidden-operator checks.
//
// If both Caps.MaxPlanCost and Caps.MaxEstimatedRows are zero AND cfg has no
// ForbiddenPlanOperators, the cost gate is a pass-through and inner.Exec is
// called directly. This makes it safe to construct with a zero CostGateConfig.
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
//  2. Otherwise run CheckPlan to obtain the plan summary. A CheckPlan error
//     rejects the query immediately with a bounded error — never silently allowed.
//  3. If the plan summary exceeds Caps.MaxPlanCost or Caps.MaxEstimatedRows, or
//     contains a ForbiddenPlanOperator, return (0, ErrPlanBudgetExceeded) without
//     calling inner.Exec.
//  4. Otherwise delegate to inner.Exec.
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
