// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package taint

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
)

// Kind identifies a sink category, for example "sql", "command", or "html". A
// sanitizer neutralizes specific kinds; a sink of kind K fires unless K was
// neutralized on every path that delivers taint to it.
type Kind string

// SourceMark marks a definition as a taint source.
type SourceMark struct {
	// Kind is the source category (for example "http_request"), recorded on
	// findings for provenance.
	Kind string
	// Label is a human-facing description of the source (for example the call or
	// parameter name).
	Label string
}

// SanitizerMark marks a statement as neutralizing a set of sink kinds for the
// value it produces. A value flowing out of the statement carries those kinds in
// its neutralized set.
type SanitizerMark struct {
	Neutralizes []Kind
}

// SinkMark marks a statement as a sink of a given kind. The statement's used
// bindings are the candidate tainted arguments.
type SinkMark struct {
	Kind  Kind
	Label string
}

// StmtBinding identifies a specific definition: a binding defined at a statement.
type StmtBinding struct {
	Stmt    int
	Binding string
}

// Facts are the per-statement taint annotations a language lowering supplies on
// top of a control-flow graph. Sources are keyed per (statement, binding) for
// precision; sanitizers and sinks are keyed per statement (the call site).
type Facts struct {
	Sources    map[StmtBinding]SourceMark
	Sanitizers map[int]SanitizerMark
	Sinks      map[int]SinkMark
}

// FindingKind distinguishes a reported taint flow from a reported sanitization.
type FindingKind string

const (
	// FindingTainted is a source value reaching a sink of a kind that was not
	// neutralized on at least one path.
	FindingTainted FindingKind = "TAINTED"
	// FindingSanitized is a source value reaching a sink whose kind was
	// neutralized on every path (the sanitizer suppressed the flow).
	FindingSanitized FindingKind = "SANITIZES"
)

// taintedConfidence and sanitizedConfidence are the fixed confidences for
// intraprocedural findings. Intraprocedural flows are direct (no cross-function
// summary composition), so they are reported with higher confidence than the
// interprocedural pass will use.
const (
	taintedConfidence   = 0.8
	sanitizedConfidence = 0.9
)

// Finding is one reported taint flow or sanitization, carrying confidence and
// provenance.
type Finding struct {
	Kind        FindingKind
	SinkKind    Kind
	SinkLabel   string
	SourceKind  string
	SourceLabel string
	Binding     string
	SourceStmt  int
	SourceLine  int
	SinkStmt    int
	SinkLine    int
	GuardReason string
	// Neutralized lists the sink kinds neutralized along the path, sorted.
	Neutralized []Kind
	Confidence  float64
}

// Limits bounds finding emission.
type Limits struct {
	MaxFindings int
}

// DefaultLimits returns the cap used when a caller supplies none.
func DefaultLimits() Limits {
	return Limits{MaxFindings: 4096}
}

func (l Limits) normalized() Limits {
	if l.MaxFindings <= 0 {
		l.MaxFindings = DefaultLimits().MaxFindings
	}
	return l
}

// Result is the bounded, deterministic output of Analyze.
type Result struct {
	Findings []Finding
	// Overflow counts findings dropped past the cap.
	Overflow int
}

// Analyze propagates taint over a resolved control-flow graph using the supplied
// source, sanitizer, and sink facts, and returns the findings. Taint flows along
// def->use chains; a value accumulates a neutralized sink-kind set, intersected
// across merging paths (a kind survives only if every path neutralized it). A
// sink of kind K reports TAINTED unless K is in the neutralized set of every
// tainted definition reaching it, in which case it reports SANITIZES. Output is
// deterministic and bounded.
func Analyze(fn cfg.Function, facts Facts, limits Limits) Result {
	limits = limits.normalized()
	g := newGraph(fn)
	states := g.propagate(facts)
	findings, overflow := g.evaluateSinks(facts, states, limits.MaxFindings)
	sortFindings(findings)
	return Result{Findings: findings, Overflow: overflow}
}

// sortFindings orders findings deterministically by sink point, source point,
// binding, kind, and sink kind.
func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.SinkStmt != b.SinkStmt {
			return a.SinkStmt < b.SinkStmt
		}
		if a.SourceStmt != b.SourceStmt {
			return a.SourceStmt < b.SourceStmt
		}
		if a.Binding != b.Binding {
			return a.Binding < b.Binding
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.SinkKind < b.SinkKind
	})
}
