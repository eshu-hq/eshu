// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cfg

// Limits bounds control-flow-graph and reaching-definition computation so a
// pathological function cannot blow up memory or emitted-fact count. The zero
// value is not useful on its own; callers should start from DefaultLimits and
// override individual caps. Every cap that trips records a counted overflow on
// the resulting Function so truncation is never silent.
type Limits struct {
	// MaxBlocks caps basic blocks per function. Past the cap the reaching-def
	// fixpoint is skipped (the CFG is still emitted) and Overflow.Blocks records
	// the total block count that triggered the skip.
	MaxBlocks int
	// MaxStmts caps statements per function. Past the cap the reaching-def
	// fixpoint is skipped and Overflow.Stmts records the total statement count.
	MaxStmts int
	// MaxDefUseEdges caps emitted def->use edges. Past the cap edge emission
	// stops in deterministic order and Overflow.DefUseEdges counts the dropped
	// edges.
	MaxDefUseEdges int
	// MaxControlDependencies caps emitted control-dependence provenance edges.
	// Past the cap emission stops in deterministic order and
	// Overflow.ControlDependencies counts the dropped edges.
	MaxControlDependencies int
	// MaxAccessPathParts caps language-lowered selector access paths. Lowerers
	// that support field-sensitive bindings truncate paths past this depth and
	// record Overflow.AccessPaths.
	MaxAccessPathParts int
}

// DefaultLimits returns the caps used when a caller does not supply its own.
// The defaults are generous enough that real Go functions never trip them while
// still bounding adversarial or generated input.
func DefaultLimits() Limits {
	return Limits{
		MaxBlocks:              4096,
		MaxStmts:               16384,
		MaxDefUseEdges:         65536,
		MaxControlDependencies: 65536,
		MaxAccessPathParts:     4,
	}
}

// normalized returns a copy with any non-positive cap replaced by the default,
// so a partially-populated Limits cannot disable a bound by accident.
func (l Limits) normalized() Limits {
	def := DefaultLimits()
	if l.MaxBlocks <= 0 {
		l.MaxBlocks = def.MaxBlocks
	}
	if l.MaxStmts <= 0 {
		l.MaxStmts = def.MaxStmts
	}
	if l.MaxDefUseEdges <= 0 {
		l.MaxDefUseEdges = def.MaxDefUseEdges
	}
	if l.MaxControlDependencies <= 0 {
		l.MaxControlDependencies = def.MaxControlDependencies
	}
	if l.MaxAccessPathParts <= 0 {
		l.MaxAccessPathParts = def.MaxAccessPathParts
	}
	return l
}
