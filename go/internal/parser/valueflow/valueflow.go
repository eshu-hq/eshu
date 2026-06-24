// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package valueflow

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/parser/taint"
)

// observeKind is the single pseudo-sink kind used to observe taint reaching any
// role-bearing statement (real sink, return, or call-argument). It starts with a
// NUL byte so it cannot collide with a real sink kind and is never neutralized by
// a sanitizer, so every reaching flow is reported; DeriveEffects then classifies
// each finding against all roles of its statement and applies real-sink
// sanitization itself using the finding's neutralized set. This avoids losing an
// effect when one statement has several roles (for example return query(arg)).
const observeKind taint.Kind = "\x00observe"

// ParamSlot locates a function parameter.
type ParamSlot struct {
	// Index is the zero-based parameter position.
	Index int
	// Stmt is the entry-block statement that defines the parameter.
	Stmt int
	// Binding is the parameter's binding name in that statement.
	Binding string
}

// SourceSlot locates an internal taint source.
type SourceSlot struct {
	// Stmt is the statement that introduces the source.
	Stmt int
	// Binding is the bound name the source defines.
	Binding string
	// Kind is the source category recorded on the SourceToReturn effect.
	Kind string
}

// SinkSlot is a real sink statement's kind.
type SinkSlot struct {
	// Kind is the sink category (for example "sql").
	Kind string
}

// CallArgSlot locates a value flowing into a callee's argument.
type CallArgSlot struct {
	// Stmt is the call statement.
	Stmt int
	// Binding is the used binding passed as the argument.
	Binding string
	// Callee is the called function's durable identity.
	Callee summary.FunctionID
	// Arg is the zero-based argument position the binding occupies.
	Arg int
}

// EffectsSpec annotates a control-flow graph with the taint roles a lowering
// recognized, so DeriveEffects can compute the function's summary. A single
// statement may carry several roles (a return whose value is a call); each role
// is honored independently.
type EffectsSpec struct {
	// Params are the function's parameters.
	Params []ParamSlot
	// Sources are internal taint sources.
	Sources []SourceSlot
	// Sinks maps a statement to its real sink kind.
	Sinks map[int]SinkSlot
	// Sanitizers maps a statement to the sink kinds it neutralizes for the value
	// it defines.
	Sanitizers map[int][]string
	// Returns lists statements that return from the function.
	Returns []int
	// CallArgs lists values flowing into callee arguments.
	CallArgs []CallArgSlot
}

// DeriveEffects computes a function's summary effects by running the
// intraprocedural taint engine from each parameter and internal source and
// classifying the reached statements against every role they carry. The result
// is deterministic.
func DeriveEffects(fn cfg.Function, spec EffectsSpec) summary.Effects {
	roles := spec.roleMaps()
	sanitizers := spec.taintSanitizers()

	var effects summary.Effects
	for _, p := range spec.Params {
		for _, f := range analyzeFrom(fn, p.Stmt, p.Binding, roles.sinks, sanitizers) {
			if f.Kind != taint.FindingTainted {
				continue
			}
			if kind, ok := roles.realSinks[f.SinkStmt]; ok && !containsKind(f.Neutralized, kind) {
				effects.ParamToSink = append(effects.ParamToSink, summary.ParamSink{
					Param: p.Index, SinkKind: kind,
				})
			}
			if roles.returnStmts[f.SinkStmt] {
				effects.ParamToReturn = appendUniqueInt(effects.ParamToReturn, p.Index)
			}
			for _, ca := range roles.callArgs[callArgKey{Stmt: f.SinkStmt, Binding: f.Binding}] {
				effects.ParamToCallArg = append(effects.ParamToCallArg, summary.CallArgFlow{
					Callee: ca.Callee, Param: p.Index, Arg: ca.Arg,
				})
			}
		}
	}
	for _, src := range spec.Sources {
		for _, f := range analyzeFrom(fn, src.Stmt, src.Binding, roles.sinks, sanitizers) {
			if f.Kind == taint.FindingTainted && roles.returnStmts[f.SinkStmt] {
				effects.SourceToReturn = appendUniqueString(effects.SourceToReturn, src.Kind)
			}
		}
	}
	normalizeEffects(&effects)
	return effects
}

// containsKind reports whether a neutralized-kind list contains kind.
func containsKind(neutralized []taint.Kind, kind string) bool {
	for _, k := range neutralized {
		if string(k) == kind {
			return true
		}
	}
	return false
}

// analyzeFrom runs the taint engine with a single origin and returns its
// findings.
func analyzeFrom(fn cfg.Function, stmt int, binding string, sinks map[int]taint.SinkMark, sanitizers map[int]taint.SanitizerMark) []taint.Finding {
	facts := taint.Facts{
		Sources:    map[taint.StmtBinding]taint.SourceMark{{Stmt: stmt, Binding: binding}: {Kind: "origin"}},
		Sinks:      sinks,
		Sanitizers: sanitizers,
	}
	return taint.Analyze(fn, facts, taint.DefaultLimits()).Findings
}

type callArgKey struct {
	Stmt    int
	Binding string
}

// roleSet holds the taint sink facts (every role-bearing statement marked with
// the single observe pseudo-sink) and the per-role lookups DeriveEffects uses to
// classify findings. A statement may appear in more than one role.
type roleSet struct {
	sinks       map[int]taint.SinkMark
	realSinks   map[int]string
	returnStmts map[int]bool
	// callArgs maps (statement, binding) to every call-argument flow it carries.
	// The same binding can occupy more than one argument position on one call
	// (for example query(r, r)), so all flows are kept, not just the last.
	callArgs map[callArgKey][]CallArgSlot
}

// roleMaps builds the role lookups. Every statement carrying any role is marked
// with observeKind so taint reaching it is always reported; the real sink kind,
// return membership, and call-arg binding are recorded separately so each role
// is classified independently.
func (spec EffectsSpec) roleMaps() roleSet {
	roles := roleSet{
		sinks:       map[int]taint.SinkMark{},
		realSinks:   map[int]string{},
		returnStmts: map[int]bool{},
		callArgs:    map[callArgKey][]CallArgSlot{},
	}
	mark := func(stmt int) { roles.sinks[stmt] = taint.SinkMark{Kind: observeKind} }
	for stmt, s := range spec.Sinks {
		mark(stmt)
		roles.realSinks[stmt] = s.Kind
	}
	for _, stmt := range spec.Returns {
		mark(stmt)
		roles.returnStmts[stmt] = true
	}
	for _, ca := range spec.CallArgs {
		mark(ca.Stmt)
		key := callArgKey{Stmt: ca.Stmt, Binding: ca.Binding}
		roles.callArgs[key] = append(roles.callArgs[key], ca)
	}
	return roles
}

// taintSanitizers converts the spec's sanitizer kinds into taint facts.
func (spec EffectsSpec) taintSanitizers() map[int]taint.SanitizerMark {
	out := map[int]taint.SanitizerMark{}
	for stmt, kinds := range spec.Sanitizers {
		marks := make([]taint.Kind, 0, len(kinds))
		for _, k := range kinds {
			marks = append(marks, taint.Kind(k))
		}
		out[stmt] = taint.SanitizerMark{Neutralizes: marks}
	}
	return out
}

// normalizeEffects sorts and de-duplicates the effect lists so the derived
// summary is deterministic.
func normalizeEffects(e *summary.Effects) {
	sort.Ints(e.ParamToReturn)
	sort.Strings(e.SourceToReturn)
	sort.Slice(e.ParamToSink, func(i, j int) bool {
		if e.ParamToSink[i].Param != e.ParamToSink[j].Param {
			return e.ParamToSink[i].Param < e.ParamToSink[j].Param
		}
		return e.ParamToSink[i].SinkKind < e.ParamToSink[j].SinkKind
	})
	e.ParamToSink = dedupeParamSink(e.ParamToSink)
	sort.Slice(e.ParamToCallArg, func(i, j int) bool {
		a, b := e.ParamToCallArg[i], e.ParamToCallArg[j]
		if a.Callee != b.Callee {
			return a.Callee < b.Callee
		}
		if a.Param != b.Param {
			return a.Param < b.Param
		}
		return a.Arg < b.Arg
	})
	e.ParamToCallArg = dedupeCallArg(e.ParamToCallArg)
}

func appendUniqueInt(xs []int, v int) []int {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}

func appendUniqueString(xs []string, v string) []string {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}

// dedupeParamSink removes adjacent duplicates from a pre-sorted slice. It reuses
// the backing array (xs[:0]); this is safe because at most one element is written
// per iteration and the write cursor never overtakes the read index.
func dedupeParamSink(xs []summary.ParamSink) []summary.ParamSink {
	out := xs[:0]
	var last summary.ParamSink
	for i, x := range xs {
		if i > 0 && x == last {
			continue
		}
		out = append(out, x)
		last = x
	}
	return out
}

func dedupeCallArg(xs []summary.CallArgFlow) []summary.CallArgFlow {
	out := xs[:0]
	var last summary.CallArgFlow
	for i, x := range xs {
		if i > 0 && x == last {
			continue
		}
		out = append(out, x)
		last = x
	}
	return out
}
