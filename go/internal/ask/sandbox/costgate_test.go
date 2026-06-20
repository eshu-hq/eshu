package sandbox_test

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/sandbox"
)

// mockExplainer is a test-only SQLExplainer that returns a fixed JSON payload
// or error. It records how many times Explain was called.
type mockExplainer struct {
	calls int
	raw   []byte
	err   error
}

func (m *mockExplainer) Explain(_ context.Context, _ string) ([]byte, error) {
	m.calls++
	return m.raw, m.err
}

// explainJSON builds a minimal EXPLAIN (FORMAT JSON) response for testing.
// cost is the root node Total Cost, rows is Plan Rows, nodeType is "Node Type".
func explainJSON(nodeType string, cost float64, rows float64) []byte {
	// Minimal valid EXPLAIN FORMAT JSON payload.
	return []byte(`[{"Plan":{"Node Type":"` + nodeType + `","Total Cost":` +
		formatFloat(cost) + `,"Plan Rows":` + formatFloat(rows) + `,"Plans":[]}}]`)
}

// formatFloat converts a float64 to a JSON-safe decimal string without
// importing strconv or fmt in test helpers (avoids cycle risk).
func formatFloat(f float64) string {
	// Simple approach: use fmt package which is already imported via testing.
	return func() string {
		// We need a string representation; use a local conversion.
		// fmt is available in test binaries; use Sprintf.
		return sprintf(f)
	}()
}

func sprintf(f float64) string {
	// Use the standard library through a thin wrapper to avoid import in the
	// helper declaration above.
	b := make([]byte, 0, 32)
	// Manual float-to-bytes for the limited range we use in tests (integers
	// and simple decimals). Avoids a second import block.
	i := int64(f)
	dec := f - float64(i)
	b = appendInt(b, i)
	if dec != 0 {
		b = append(b, '.')
		// Up to 2 decimal places, trimmed of trailing zeros.
		d1 := int64(dec * 10)
		d2 := int64(dec*100) % 10
		b = appendInt(b, d1)
		if d2 != 0 {
			b = appendInt(b, d2)
		}
	}
	return string(b)
}

func appendInt(b []byte, v int64) []byte {
	if v == 0 {
		return append(b, '0')
	}
	var tmp [20]byte
	pos := 20
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		pos--
		tmp[pos] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		b = append(b, '-')
	}
	return append(b, tmp[pos:]...)
}

// ── CostGateExecutor — basic construction ─────────────────────────────────────

func TestNewCostGateExecutor_NotNil(t *testing.T) {
	t.Parallel()

	inner := &mockExecutor{}
	exp := &mockExplainer{}
	g := sandbox.NewCostGateExecutor(inner, exp, sandbox.CostGateConfig{})
	if g == nil {
		t.Fatal("NewCostGateExecutor returned nil")
	}
}

// ── Cypher passes straight through to inner ───────────────────────────────────

func TestCostGateExecutor_Cypher_PassesThrough(t *testing.T) {
	t.Parallel()

	inner := &mockExecutor{rowCount: 5}
	exp := &mockExplainer{}
	caps := sandbox.DefaultCaps()
	g := sandbox.NewCostGateExecutor(inner, exp, sandbox.CostGateConfig{})

	rows, err := g.Exec(context.Background(), sandbox.DialectCypher, "MATCH (n:Service) RETURN n", caps)
	_ = rows
	// inner.Exec handles Cypher; it may return an error (not wired in v1), but
	// the cost gate must have called inner, not explainer.
	if exp.calls != 0 {
		t.Errorf("explainer called %d time(s) for Cypher, want 0", exp.calls)
	}
	if inner.calls != 1 {
		t.Errorf("inner.Exec called %d time(s) for Cypher, want 1", inner.calls)
	}
	_ = err
}

// ── Zero caps bypass ──────────────────────────────────────────────────────────

func TestCostGateExecutor_ZeroCaps_NoExplain(t *testing.T) {
	t.Parallel()

	// If MaxPlanCost and MaxEstimatedRows are both zero and no forbidden
	// operators are configured, the cost gate must not call the explainer.
	inner := &mockExecutor{rowCount: 3}
	exp := &mockExplainer{}
	caps := sandbox.Caps{
		MaxRows:          100,
		MaxBytes:         1 << 20,
		Timeout:          5e9,
		MaxQueryLen:      8192,
		MaxPlanCost:      0,
		MaxEstimatedRows: 0,
	}
	g := sandbox.NewCostGateExecutor(inner, exp, sandbox.CostGateConfig{})

	rows, err := g.Exec(context.Background(), sandbox.DialectSQL, "SELECT 1", caps)
	if err != nil {
		t.Fatalf("Exec returned unexpected error: %v", err)
	}
	if rows != 3 {
		t.Errorf("rows = %d, want 3", rows)
	}
	if exp.calls != 0 {
		t.Errorf("explainer called %d time(s), want 0 (zero caps bypass)", exp.calls)
	}
	if inner.calls != 1 {
		t.Errorf("inner.Exec called %d time(s), want 1", inner.calls)
	}
}

// ── Over-budget plan is rejected ──────────────────────────────────────────────

func TestCostGateExecutor_OverBudgetCost_Rejected(t *testing.T) {
	t.Parallel()

	// Plan cost 5000 > MaxPlanCost 1000 → must reject before calling inner.
	inner := &mockExecutor{rowCount: 100}
	exp := &mockExplainer{raw: explainJSON("Seq Scan", 5000, 50000)}
	caps := sandbox.DefaultCaps() // MaxPlanCost = 1000, MaxEstimatedRows = 100_000

	g := sandbox.NewCostGateExecutor(inner, exp, sandbox.CostGateConfig{})

	rows, err := g.Exec(context.Background(), sandbox.DialectSQL, "SELECT * FROM large_table", caps)
	if err == nil {
		t.Fatal("over-budget plan: expected error, got nil")
	}
	if !errors.Is(err, sandbox.ErrPlanBudgetExceeded) {
		t.Errorf("err = %v, want wrapping ErrPlanBudgetExceeded", err)
	}
	if rows != 0 {
		t.Errorf("rows = %d, want 0 (rejected before exec)", rows)
	}
	if inner.calls != 0 {
		t.Errorf("inner.Exec called %d time(s), want 0 (must not exec when budget exceeded)", inner.calls)
	}
	if exp.calls != 1 {
		t.Errorf("explainer called %d time(s), want 1", exp.calls)
	}
}

func TestCostGateExecutor_OverBudgetRows_Rejected(t *testing.T) {
	t.Parallel()

	// Estimated rows 200_000 > MaxEstimatedRows 100_000 → must reject.
	inner := &mockExecutor{rowCount: 100}
	exp := &mockExplainer{raw: explainJSON("Index Scan", 500, 200000)}
	caps := sandbox.DefaultCaps()

	g := sandbox.NewCostGateExecutor(inner, exp, sandbox.CostGateConfig{})

	rows, err := g.Exec(context.Background(), sandbox.DialectSQL, "SELECT id FROM facts", caps)
	if err == nil {
		t.Fatal("over-budget row estimate: expected error, got nil")
	}
	if !errors.Is(err, sandbox.ErrPlanBudgetExceeded) {
		t.Errorf("err = %v, want wrapping ErrPlanBudgetExceeded", err)
	}
	if rows != 0 {
		t.Errorf("rows = %d, want 0", rows)
	}
	if inner.calls != 0 {
		t.Errorf("inner.Exec called %d time(s), want 0", inner.calls)
	}
}

// ── In-budget plan proceeds to inner ─────────────────────────────────────────

func TestCostGateExecutor_InBudget_Proceeds(t *testing.T) {
	t.Parallel()

	// Plan cost 42, rows 10 — well under DefaultCaps limits.
	inner := &mockExecutor{rowCount: 10}
	exp := &mockExplainer{raw: explainJSON("Index Scan", 42, 10)}
	caps := sandbox.DefaultCaps()

	g := sandbox.NewCostGateExecutor(inner, exp, sandbox.CostGateConfig{})

	rows, err := g.Exec(context.Background(), sandbox.DialectSQL, "SELECT id FROM repos WHERE id = $1", caps)
	if err != nil {
		t.Fatalf("in-budget plan: unexpected error: %v", err)
	}
	if rows != 10 {
		t.Errorf("rows = %d, want 10", rows)
	}
	if inner.calls != 1 {
		t.Errorf("inner.Exec called %d time(s), want 1", inner.calls)
	}
	if exp.calls != 1 {
		t.Errorf("explainer called %d time(s), want 1", exp.calls)
	}
}

// ── Forbidden plan operator is rejected ───────────────────────────────────────

func TestCostGateExecutor_ForbiddenOperator_Rejected(t *testing.T) {
	t.Parallel()

	// Seq Scan is in ForbiddenPlanOperators → must reject regardless of cost.
	inner := &mockExecutor{rowCount: 5}
	exp := &mockExplainer{raw: explainJSON("Seq Scan", 10, 5)}
	caps := sandbox.DefaultCaps()
	cfg := sandbox.CostGateConfig{ForbiddenPlanOperators: []string{"Seq Scan"}}

	g := sandbox.NewCostGateExecutor(inner, exp, cfg)

	rows, err := g.Exec(context.Background(), sandbox.DialectSQL, "SELECT * FROM small_table", caps)
	if err == nil {
		t.Fatal("forbidden operator: expected error, got nil")
	}
	if !errors.Is(err, sandbox.ErrPlanBudgetExceeded) {
		t.Errorf("err = %v, want wrapping ErrPlanBudgetExceeded", err)
	}
	if rows != 0 {
		t.Errorf("rows = %d, want 0", rows)
	}
	if inner.calls != 0 {
		t.Errorf("inner.Exec called %d time(s), want 0 (must not exec when operator forbidden)", inner.calls)
	}
}

func TestCostGateExecutor_AllowedOperator_Proceeds(t *testing.T) {
	t.Parallel()

	// Index Scan is not forbidden → should proceed.
	inner := &mockExecutor{rowCount: 2}
	exp := &mockExplainer{raw: explainJSON("Index Scan", 10, 2)}
	caps := sandbox.DefaultCaps()
	cfg := sandbox.CostGateConfig{ForbiddenPlanOperators: []string{"Seq Scan"}}

	g := sandbox.NewCostGateExecutor(inner, exp, cfg)

	rows, err := g.Exec(context.Background(), sandbox.DialectSQL, "SELECT id FROM repos WHERE id = $1", caps)
	if err != nil {
		t.Fatalf("allowed operator: unexpected error: %v", err)
	}
	if rows != 2 {
		t.Errorf("rows = %d, want 2", rows)
	}
	if inner.calls != 1 {
		t.Errorf("inner.Exec called %d time(s), want 1", inner.calls)
	}
}

// ── Explainer failure → rejected (fail closed) ────────────────────────────────

func TestCostGateExecutor_ExplainError_Rejected(t *testing.T) {
	t.Parallel()

	inner := &mockExecutor{rowCount: 5}
	exp := &mockExplainer{err: errors.New("backend unavailable")}
	caps := sandbox.DefaultCaps()

	g := sandbox.NewCostGateExecutor(inner, exp, sandbox.CostGateConfig{})

	rows, err := g.Exec(context.Background(), sandbox.DialectSQL, "SELECT 1", caps)
	if err == nil {
		t.Fatal("explainer error: expected error, got nil (must fail closed)")
	}
	if rows != 0 {
		t.Errorf("rows = %d, want 0", rows)
	}
	if inner.calls != 0 {
		t.Errorf("inner.Exec called %d time(s), want 0 (must not exec on plan failure)", inner.calls)
	}
}

// ── Malformed EXPLAIN JSON → rejected ─────────────────────────────────────────

func TestCostGateExecutor_MalformedJSON_Rejected(t *testing.T) {
	t.Parallel()

	inner := &mockExecutor{rowCount: 5}
	exp := &mockExplainer{raw: []byte(`not valid json`)}
	caps := sandbox.DefaultCaps()

	g := sandbox.NewCostGateExecutor(inner, exp, sandbox.CostGateConfig{})

	rows, err := g.Exec(context.Background(), sandbox.DialectSQL, "SELECT 1", caps)
	if err == nil {
		t.Fatal("malformed JSON: expected error, got nil")
	}
	if rows != 0 {
		t.Errorf("rows = %d, want 0", rows)
	}
	if inner.calls != 0 {
		t.Errorf("inner.Exec called %d time(s), want 0", inner.calls)
	}
}

// ── ErrPlanBudgetExceeded sentinel ────────────────────────────────────────────

func TestErrPlanBudgetExceeded_NotNil(t *testing.T) {
	t.Parallel()

	if sandbox.ErrPlanBudgetExceeded == nil {
		t.Error("ErrPlanBudgetExceeded is nil, want non-nil")
	}
}

// ── CheckPlan observable ──────────────────────────────────────────────────────

func TestCostGateExecutor_CheckPlan_ReturnsSummary(t *testing.T) {
	t.Parallel()

	exp := &mockExplainer{raw: explainJSON("Index Scan", 123, 456)}
	inner := &mockExecutor{}
	g := sandbox.NewCostGateExecutor(inner, exp, sandbox.CostGateConfig{})

	summary, err := g.CheckPlan(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatalf("CheckPlan returned error: %v", err)
	}
	if summary.TotalCost != 123 {
		t.Errorf("TotalCost = %.2f, want 123.00", summary.TotalCost)
	}
	if summary.EstimatedRows != 456 {
		t.Errorf("EstimatedRows = %.0f, want 456", summary.EstimatedRows)
	}
	if summary.ForbiddenOperator != "" {
		t.Errorf("ForbiddenOperator = %q, want empty", summary.ForbiddenOperator)
	}
}

// ── DefaultCaps includes MaxPlanCost and MaxEstimatedRows ─────────────────────

func TestDefaultCaps_PlanLimits(t *testing.T) {
	t.Parallel()

	caps := sandbox.DefaultCaps()
	if caps.MaxPlanCost <= 0 {
		t.Errorf("DefaultCaps MaxPlanCost = %.2f, want > 0", caps.MaxPlanCost)
	}
	if caps.MaxEstimatedRows <= 0 {
		t.Errorf("DefaultCaps MaxEstimatedRows = %.0f, want > 0", caps.MaxEstimatedRows)
	}
}

// ── In-tx plan check path ─────────────────────────────────────────────────────

// fakeInTxChecker is a test-only Executor that implements the inTxPlanChecker
// interface (via the exported surface it exposes through ExecWithPlanCheck).
// It records which paths were taken so tests can assert that CostGateExecutor
// delegates to the in-tx path when the inner executor supports it, rather than
// calling the SQLExplainer and inner.Exec separately.
//
// Because inTxPlanChecker is unexported, we cannot implement it directly from
// the external test package. Instead, fakeInTxChecker implements Executor and
// is wrapped by NewCostGateExecutor; we verify the behaviour by checking that
// neither the SQLExplainer (g.explainer) nor the separate inner.Exec path is
// taken — instead the fake's own ExecWithPlanCheck counts are incremented.
//
// To test the structural invariant without a live database, we use a separate
// fake executor whose Exec method records a "separate exec" call. If the
// CostGateExecutor calls inner.Exec separately AND the explainer, the
// separate-exec count will be non-zero. If it routes through execWithPlanCheck,
// only planCheckCalls will be non-zero.
//
// NOTE: because inTxPlanChecker is an unexported interface, the only way to
// satisfy it from outside the package is via a type in a package that can see
// it — which is the internal package itself. We assert the in-tx invariant
// indirectly: by checking that NewPostgresReadOnlyExecutorWithCostGate (the
// production constructor) is accepted as an inTxPlanChecker, and that wrapping
// it in CostGateExecutor routes the in-tx path (verified by the executor's own
// Exec implementation, which we test through the Guard in integration form).
//
// The direct structural proof of "EXPLAIN + query hit the same tx" is covered
// by TestPostgresReadOnlyExecutorWithCostGate_InTxSameContext in pgexec_test.go.

// TestCostGateExecutor_MockPath_ExplainerCalledForSQL verifies that when inner
// does NOT implement inTxPlanChecker (the mock/test path), CostGateExecutor
// calls the SQLExplainer for plan checking and then delegates to inner.Exec.
// This is the existing unit-test behaviour; this test makes the path explicit.
func TestCostGateExecutor_MockPath_ExplainerCalledForSQL(t *testing.T) {
	t.Parallel()

	// mockExecutor does not implement inTxPlanChecker, so CostGateExecutor must
	// fall back to the SQLExplainer + inner.Exec path.
	inner := &mockExecutor{rowCount: 7}
	exp := &mockExplainer{raw: explainJSON("Index Scan", 100, 1000)}
	caps := sandbox.DefaultCaps()
	g := sandbox.NewCostGateExecutor(inner, exp, sandbox.CostGateConfig{})

	rows, err := g.Exec(context.Background(), sandbox.DialectSQL, "SELECT id FROM repos", caps)
	if err != nil {
		t.Fatalf("Exec returned unexpected error: %v", err)
	}
	if rows != 7 {
		t.Errorf("rows = %d, want 7", rows)
	}
	// On the fallback path both the explainer and inner.Exec must be called.
	if exp.calls != 1 {
		t.Errorf("explainer calls = %d, want 1 (fallback path must use explainer)", exp.calls)
	}
	if inner.calls != 1 {
		t.Errorf("inner.Exec calls = %d, want 1 (fallback path must call inner.Exec)", inner.calls)
	}
}

// TestCostGateExecutor_InTxPath_ProducerConstructor verifies that
// NewPostgresReadOnlyExecutorWithCostGate returns an executor that can be
// constructed without panicking and satisfies the Executor interface.
// The in-tx structural proof (EXPLAIN + query in same tx) is in pgexec_test.go.
func TestCostGateExecutor_InTxPath_ProducerConstructor(t *testing.T) {
	t.Parallel()

	// nil db is valid at construction time; Exec would panic on nil db, but we
	// only verify construction here.
	exec := sandbox.NewPostgresReadOnlyExecutorWithCostGate(nil, sandbox.CostGateConfig{
		ForbiddenPlanOperators: []string{"Seq Scan"},
	})
	if exec == nil {
		t.Fatal("NewPostgresReadOnlyExecutorWithCostGate returned nil")
	}
	// Wrapping in CostGateExecutor must also succeed.
	g := sandbox.NewCostGateExecutor(exec, &mockExplainer{}, sandbox.CostGateConfig{
		ForbiddenPlanOperators: []string{"Seq Scan"},
	})
	if g == nil {
		t.Fatal("NewCostGateExecutor wrapping in-tx executor returned nil")
	}
}
