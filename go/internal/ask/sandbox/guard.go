// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sandbox

import "context"

// Executor executes a validated, sandboxed query against a backend and returns
// the number of rows observed. Implementations MUST NOT execute the query
// before Validate returns an allowed Decision; the Guard enforces this contract
// so individual Executor implementations do not need to re-validate.
//
// Implementations are responsible for enforcing caps.Timeout and caps.MaxRows
// on the backend side as defense-in-depth after the Guard has already applied
// those limits.
type Executor interface {
	// Exec runs the query against the backend under the given dialect and caps.
	// It returns the number of rows returned by the query (truncated to
	// caps.MaxRows) and any execution error. The context carries the caller
	// deadline; Exec must respect it.
	Exec(ctx context.Context, dialect Dialect, query string, caps Caps) (rowCount int, err error)
}

// Validate dispatches query authorization to the appropriate dialect validator.
// It is the single entry point for query authorization in the sandbox.
//
// Policy (evaluated in order):
//  1. len(query) > caps.MaxQueryLen → deny "query exceeds maximum length".
//  2. DialectCypher → validateCypher(query).
//  3. DialectSQL    → validateSQL(query).
//  4. Any other dialect → deny "unsupported dialect".
//
// The deny Reason is always bounded: it never echoes the query body and uses
// only fixed strings or single keyword tokens.
func Validate(dialect Dialect, query string, caps Caps) Decision {
	if len(query) > caps.MaxQueryLen {
		return Decision{Allowed: false, Reason: "query exceeds maximum length"}
	}
	switch dialect {
	case DialectCypher:
		return validateCypher(query)
	case DialectSQL:
		return validateSQL(query)
	default:
		return Decision{Allowed: false, Reason: "unsupported dialect"}
	}
}

// Guard is the top-level sandbox entry point. It applies the enabled check,
// Validate, and Executor dispatch in order. The zero value of Guard is
// NOT usable; always construct via NewGuard.
//
// Guard is safe for concurrent use after construction.
type Guard struct {
	exec    Executor
	caps    Caps
	enabled bool
}

// NewGuard constructs a Guard. If enabled is false the Guard always returns
// ErrSandboxDisabled without contacting the Executor. If caps is the zero
// value, DefaultCaps() is used so that a zero MaxQueryLen does not accidentally
// deny every query.
func NewGuard(exec Executor, caps Caps, enabled bool) *Guard {
	if caps == (Caps{}) {
		caps = DefaultCaps()
	}
	return &Guard{exec: exec, caps: caps, enabled: enabled}
}

// Run is the main sandbox entry point. It enforces the following layered
// policy:
//
//  1. Enabled check: if the Guard was constructed with enabled=false, Run
//     returns Decision{false,"sandbox disabled"}, 0, ErrSandboxDisabled
//     immediately. The Executor is NEVER called.
//
//  2. Authorization: Validate(dialect, query, caps) is called. If the decision
//     is denied, Run returns that Decision, 0, nil. The Executor is NOT called.
//
//  3. Execution: g.exec.Exec(ctx, dialect, query, caps) is called. Run returns
//     Decision{Allowed:true} with the row count and any execution error.
//
// The returned error is non-nil only when the sandbox is disabled (step 1) or
// the Executor returns an error (step 3). A denied authorization (step 2) is
// NOT an error: the caller may log the Decision.Reason and return it to the
// user as a bounded message.
func (g *Guard) Run(ctx context.Context, dialect Dialect, query string) (Decision, int, error) {
	if !g.enabled {
		return Decision{Allowed: false, Reason: "sandbox disabled"}, 0, ErrSandboxDisabled
	}

	d := Validate(dialect, query, g.caps)
	if !d.Allowed {
		return d, 0, nil
	}

	rows, err := g.exec.Exec(ctx, dialect, query, g.caps)
	return Decision{Allowed: true}, rows, err
}
