package sandbox

import (
	"errors"
	"time"
)

// Dialect represents the query language dialect for the sandbox.
type Dialect string

// DialectCypher represents the Cypher query language dialect.
const DialectCypher Dialect = "cypher"

// DialectSQL represents the SQL query language dialect.
const DialectSQL Dialect = "sql"

// Decision represents the authorization decision for a sandboxed query.
// When Allowed is true, Reason must be empty. When Allowed is false, Reason
// contains a bounded, low-cardinality explanation that does not echo the query
// or reveal secrets.
type Decision struct {
	// Allowed indicates whether the query is authorized to execute.
	Allowed bool
	// Reason is empty when Allowed is true; a bounded reason string when denied.
	Reason string
}

// Caps defines resource capacity constraints for sandboxed query execution.
type Caps struct {
	// MaxRows is the maximum number of rows a query may return.
	MaxRows int
	// MaxBytes is the maximum bytes a query result may consume.
	MaxBytes int
	// Timeout is the maximum time a query may execute before cancellation.
	Timeout time.Duration
	// MaxQueryLen is the maximum length in bytes of a query string.
	MaxQueryLen int
	// MaxPlanCost is the maximum total estimated cost that a SQL query plan may
	// carry. A plan whose root node's Total Cost exceeds this value is rejected
	// before execution with reason "query plan exceeds cost budget". Zero means
	// no cost limit is applied. The Postgres planner's cost unit is arbitrary
	// but stable within a cluster; a conservative default of 1000.0 is chosen
	// to block full-table sequential scans on large relations.
	MaxPlanCost float64
	// MaxEstimatedRows is the maximum number of rows the planner estimates the
	// query will return. A plan whose root-node row estimate exceeds this value
	// is rejected before execution. Zero means no row-estimate limit is applied.
	// This is distinct from MaxRows, which truncates actual output after
	// execution; MaxEstimatedRows stops expensive plans before they run.
	MaxEstimatedRows float64
}

// DefaultCaps returns the default capacity constraints for sandbox queries.
func DefaultCaps() Caps {
	return Caps{
		MaxRows:          1000,
		MaxBytes:         1 << 20, // 1 MiB
		Timeout:          5 * time.Second,
		MaxQueryLen:      8192,
		MaxPlanCost:      1000.0,  // blocks full-table scans on large relations
		MaxEstimatedRows: 100_000, // estimated rows before execution
	}
}

// ErrSandboxDisabled is returned when a sandbox operation is attempted but
// the sandbox is not enabled. This is the default-off security posture.
var ErrSandboxDisabled = errors.New("ask/sandbox: sandbox is disabled")
