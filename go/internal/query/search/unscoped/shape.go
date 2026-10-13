// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

import "github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

// key is a table shape's resume cursor: the last visited key in the shape's
// order. Its zero value starts a walk at the first row; it must be comparable
// so the engine can tell whether a call advanced it.
type key interface {
	comparable
	// IsZero reports whether the key names no row.
	IsZero() bool
}

// shape is one table the engine walks: its span identity, its window sizes
// and phase figures, its four bounded statements, how its cursor binds, and
// how a result row is scanned. The engine (walk.go) owns every budget,
// savepoint, timeout and cursor invariant and never names a table; a shape
// owns only what differs between tables. R is the row type, K the key type.
//
// Statement argument contract, shared by every shape so the engine can bind
// them: a step statement takes $1 pattern, $2 window rows, $3 matches wanted;
// a tail statement takes $1 pattern, $2 matches wanted. The "after" forms
// take the key columns next, in the order keyArgs returns them. A step returns
// rows of kind kindHit and at most one row of kind kindEdge, the window's last
// key, absent when the window held fewer rows than asked.
type shape[R any, K key] struct {
	// table is the db.sql.table span attribute.
	table string
	// operation is the db.operation span attribute.
	operation string
	// subject names what the walk searches in its error text
	// ("search unscoped <subject>: ...").
	subject string
	// figures are the window sizes and phase limits at DefaultBudget. Every
	// field must be positive: the engine divides by probeRows when predicting
	// the first continuation step. TestShapeFiguresArePositive pins that for
	// every shape.
	figures figures

	stepFirst, stepAfter string // key-ordered window, from the start or after a key
	tailFirst, tailAfter string // trigram tail, from the start or after a key

	// keyArgs returns the "after" statements' key arguments for a cursor.
	keyArgs func(K) []any
	// scan reads one result row: its kind (kindHit or kindEdge) and the row.
	scan func(db.Rows) (string, R, error)
	// edgeKey returns the key of an edge row.
	edgeKey func(R) K
}

// partial is the engine's view of a walk the budget ended. A shape turns it
// into its wire type; every field is shape-independent except the cursor.
type partial[K key] struct {
	reason             string
	rowsScannedInOrder int
	rowsMatched        int
	cursor             K
	budgetMS           int64
	elapsedMS          int64
	overrunMS          int64
	progressed         bool
	hint               string
}
