// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

// EntityStatementSet is the shipped text of the four bounded entity
// statements (the content_entities shape, entities.go), exported so the live plan-shape and differential proofs derive
// their statements from the constants production runs instead of copying them.
type EntityStatementSet struct {
	// StepFirst and StepAfter are the key-ordered window statements for the
	// first window and for a window after a cursor.
	StepFirst, StepAfter string
	// TailFirst and TailAfter are the trigram tail statements.
	TailFirst, TailAfter string
	// DisableIndexScan is the SET LOCAL the tail runs under.
	DisableIndexScan string
}

// EntityStatements returns the shipped entity statement text.
func EntityStatements() EntityStatementSet {
	return EntityStatementSet{
		StepFirst:        entityStepFirstSQL,
		StepAfter:        entityStepAfterSQL,
		TailFirst:        entityTailFirstSQL,
		TailAfter:        entityTailAfterSQL,
		DisableIndexScan: disableIndexScanSQL,
	}
}
