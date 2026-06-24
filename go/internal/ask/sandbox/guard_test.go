// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sandbox_test

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/sandbox"
)

// mockExecutor is a test-only Executor that records how many times Exec was
// called and returns a fixed result.
type mockExecutor struct {
	calls    int
	rowCount int
	err      error
}

func (m *mockExecutor) Exec(_ context.Context, _ sandbox.Dialect, _ string, _ sandbox.Caps) (int, error) {
	m.calls++
	return m.rowCount, m.err
}

// ── Validate dispatch tests ───────────────────────────────────────────────────

func TestValidate_CypherAllowed(t *testing.T) {
	t.Parallel()

	caps := sandbox.DefaultCaps()
	d := sandbox.Validate(sandbox.DialectCypher, "MATCH (n:Service) RETURN n LIMIT 10", caps)
	if !d.Allowed {
		t.Errorf("Validate(DialectCypher, allowed query) = denied: %q", d.Reason)
	}
	if d.Reason != "" {
		t.Errorf("Validate allowed: Reason must be empty, got %q", d.Reason)
	}
}

func TestValidate_SQLDenied(t *testing.T) {
	t.Parallel()

	caps := sandbox.DefaultCaps()
	d := sandbox.Validate(sandbox.DialectSQL, "DELETE FROM users", caps)
	if d.Allowed {
		t.Error("Validate(DialectSQL, DELETE) = allowed, want denied")
	}
	if d.Reason == "" {
		t.Error("Validate denied: Reason must not be empty")
	}
}

func TestValidate_ExceedsMaxQueryLen(t *testing.T) {
	t.Parallel()

	caps := sandbox.DefaultCaps()
	long := make([]byte, caps.MaxQueryLen+1)
	for i := range long {
		long[i] = 'x'
	}
	d := sandbox.Validate(sandbox.DialectSQL, string(long), caps)
	if d.Allowed {
		t.Error("Validate over MaxQueryLen = allowed, want denied")
	}
	if d.Reason != "query exceeds maximum length" {
		t.Errorf("Reason = %q, want %q", d.Reason, "query exceeds maximum length")
	}
}

func TestValidate_UnsupportedDialect(t *testing.T) {
	t.Parallel()

	caps := sandbox.DefaultCaps()
	d := sandbox.Validate(sandbox.Dialect("graphql"), "{ foo }", caps)
	if d.Allowed {
		t.Error("Validate(unsupported dialect) = allowed, want denied")
	}
	if d.Reason != "unsupported dialect" {
		t.Errorf("Reason = %q, want %q", d.Reason, "unsupported dialect")
	}
}

// ── Guard disabled tests ──────────────────────────────────────────────────────

func TestGuard_Disabled_NeverExecs(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{rowCount: 42}
	g := sandbox.NewGuard(exec, sandbox.DefaultCaps(), false)

	d, rows, err := g.Run(context.Background(), sandbox.DialectSQL, "SELECT 1")
	if d.Allowed {
		t.Error("disabled Guard.Run = allowed, want denied")
	}
	if d.Reason != "sandbox disabled" {
		t.Errorf("Reason = %q, want %q", d.Reason, "sandbox disabled")
	}
	if !errors.Is(err, sandbox.ErrSandboxDisabled) {
		t.Errorf("err = %v, want ErrSandboxDisabled", err)
	}
	if rows != 0 {
		t.Errorf("rows = %d, want 0", rows)
	}
	if exec.calls != 0 {
		t.Errorf("exec called %d time(s), want 0 (must not exec when disabled)", exec.calls)
	}
}

// ── Guard enabled + allowed tests ─────────────────────────────────────────────

func TestGuard_Enabled_AllowedSQL_ExecsOnce(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{rowCount: 7}
	g := sandbox.NewGuard(exec, sandbox.DefaultCaps(), true)

	d, rows, err := g.Run(context.Background(), sandbox.DialectSQL, "SELECT 1")
	if !d.Allowed {
		t.Errorf("enabled Guard.Run(SELECT) = denied: %q", d.Reason)
	}
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if rows != 7 {
		t.Errorf("rows = %d, want 7", rows)
	}
	if exec.calls != 1 {
		t.Errorf("exec called %d time(s), want exactly 1", exec.calls)
	}
}

// ── Guard enabled + denied tests ─────────────────────────────────────────────

func TestGuard_Enabled_DeniedSQL_DoesNotExec(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{rowCount: 99}
	g := sandbox.NewGuard(exec, sandbox.DefaultCaps(), true)

	d, rows, err := g.Run(context.Background(), sandbox.DialectSQL, "DELETE FROM users")
	if d.Allowed {
		t.Error("enabled Guard.Run(DELETE) = allowed, want denied")
	}
	if d.Reason == "" {
		t.Error("denied Guard.Run: Reason must not be empty")
	}
	if err != nil {
		t.Errorf("err = %v, want nil (no exec attempted)", err)
	}
	if rows != 0 {
		t.Errorf("rows = %d, want 0 (no exec attempted)", rows)
	}
	if exec.calls != 0 {
		t.Errorf("exec called %d time(s), want 0 (must not exec when denied)", exec.calls)
	}
}

func TestGuard_Enabled_OverLength_DoesNotExec(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	caps := sandbox.DefaultCaps()
	g := sandbox.NewGuard(exec, caps, true)

	long := make([]byte, caps.MaxQueryLen+1)
	for i := range long {
		long[i] = 'x'
	}
	d, rows, err := g.Run(context.Background(), sandbox.DialectSQL, string(long))
	if d.Allowed {
		t.Error("over-length Guard.Run = allowed, want denied")
	}
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if rows != 0 {
		t.Errorf("rows = %d, want 0", rows)
	}
	if exec.calls != 0 {
		t.Errorf("exec called %d time(s), want 0", exec.calls)
	}
}

func TestGuard_Enabled_UnknownDialect_DoesNotExec(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	g := sandbox.NewGuard(exec, sandbox.DefaultCaps(), true)

	d, rows, err := g.Run(context.Background(), sandbox.Dialect("sparql"), "SELECT ?x WHERE { ?x a :Thing }")
	if d.Allowed {
		t.Error("unknown dialect Guard.Run = allowed, want denied")
	}
	if d.Reason != "unsupported dialect" {
		t.Errorf("Reason = %q, want %q", d.Reason, "unsupported dialect")
	}
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if rows != 0 {
		t.Errorf("rows = %d, want 0", rows)
	}
	if exec.calls != 0 {
		t.Errorf("exec called %d time(s), want 0", exec.calls)
	}
}

// ── NewGuard zero caps test ───────────────────────────────────────────────────

func TestNewGuard_ZeroCaps_UsesDefaults(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{rowCount: 1}
	// Passing zero Caps; the Guard should promote them to DefaultCaps so that a
	// valid short query is not rejected by a zero MaxQueryLen.
	g := sandbox.NewGuard(exec, sandbox.Caps{}, true)

	d, _, err := g.Run(context.Background(), sandbox.DialectSQL, "SELECT 1")
	if !d.Allowed {
		t.Errorf("NewGuard(zeroCaps).Run = denied: %q", d.Reason)
	}
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}
