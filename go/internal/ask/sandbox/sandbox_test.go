package sandbox_test

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/ask/sandbox"
)

func TestDialectConstants(t *testing.T) {
	t.Parallel()

	if sandbox.DialectCypher != "cypher" {
		t.Errorf("DialectCypher = %q, want %q", sandbox.DialectCypher, "cypher")
	}
	if sandbox.DialectSQL != "sql" {
		t.Errorf("DialectSQL = %q, want %q", sandbox.DialectSQL, "sql")
	}
}

func TestDecisionZeroValue(t *testing.T) {
	t.Parallel()

	var d sandbox.Decision
	if d.Allowed != false {
		t.Errorf("Decision zero-value Allowed = %v, want false", d.Allowed)
	}
	if d.Reason != "" {
		t.Errorf("Decision zero-value Reason = %q, want empty", d.Reason)
	}
}

func TestDefaultCaps(t *testing.T) {
	t.Parallel()

	caps := sandbox.DefaultCaps()
	if caps.MaxRows != 1000 {
		t.Errorf("DefaultCaps MaxRows = %d, want 1000", caps.MaxRows)
	}
	if caps.MaxBytes != (1 << 20) {
		t.Errorf("DefaultCaps MaxBytes = %d, want %d", caps.MaxBytes, 1<<20)
	}
	if caps.Timeout != 5*time.Second {
		t.Errorf("DefaultCaps Timeout = %v, want %v", caps.Timeout, 5*time.Second)
	}
	if caps.MaxQueryLen != 8192 {
		t.Errorf("DefaultCaps MaxQueryLen = %d, want 8192", caps.MaxQueryLen)
	}
}

func TestErrSandboxDisabled(t *testing.T) {
	t.Parallel()

	if sandbox.ErrSandboxDisabled == nil {
		t.Error("ErrSandboxDisabled is nil, want non-nil")
	}
	if sandbox.ErrSandboxDisabled.Error() != "ask/sandbox: sandbox is disabled" {
		t.Errorf("ErrSandboxDisabled.Error() = %q, want %q", sandbox.ErrSandboxDisabled.Error(), "ask/sandbox: sandbox is disabled")
	}
}
