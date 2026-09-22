// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package dataflow lowers a Go function, method, or function literal body into
// a control-flow graph and resolves reaching definitions over it, reusing the
// language-neutral internal/parser/cfg engine. It is the Go counterpart of the
// TS/JS lowering in internal/parser/javascript/jsdataflow, which documents the
// shared design in more depth; this package mirrors its file layout (lower.go,
// bindings.go, access_paths.go).
//
// Control flow is lowered precisely for blocks, if/else, and for loops
// (including for-range); constructs not modeled precisely yet (switch, select)
// contribute their identifier uses but no definitions, which can miss a
// reaching definition but never invents a false edge. Parameters and the
// method receiver are modeled as definitions in the entry block so value flow
// from a parameter into the body is captured.
//
// Bindings are field-sensitive (access_paths.go): a selector target
// data.SQL = x defines the access path data.SQL, a subscript m[k] lowers to
// the explicitly labeled whole-container approximation m[*], and a field
// write through a simple pointer alias (alias := &data; alias.SQL = x)
// normalizes to the aliased struct. Paths deeper than
// cfg.Limits.MaxAccessPathParts truncate to a "*"-suffixed prefix and count
// Overflow.AccessPaths, never a silent drop. A function literal passed as a
// call argument is descended into to attribute its captured (free) variables
// to the enclosing function, excluding the closure's own parameters and
// inner-scope definitions.
//
// The result is bounded and deterministic: the cfg engine sorts its output and
// records counted overflow rather than dropping data silently.
//
// Intraprocedural taint annotations (sources, sinks, sanitizers) are derived
// from a small, conservative Go catalog (taint_facts.go) mapped onto the
// control-flow graph, ready for the internal/parser/taint engine. Guard
// predicates for if/for conditions are captured as redacted,
// whitespace-normalized text (guard_text.go) for control-dependence
// explanations, without carrying source literal values.
//
// EmitBuckets and InterprocPayloads are the two entry points the golang
// parser (Parse in language.go) calls when Options.EmitDataflow is set.
// EmitBuckets renders the per-function "dataflow_functions" and
// "taint_findings" payload buckets. InterprocPayloads composes per-function
// value-flow summaries into an interprocedural port graph and renders
// "interproc_findings", "dataflow_summaries", and "dataflow_sources".
//
// This package never imports internal/parser/golang — that back-edge is the
// import cycle issue #6774 removes. A symbol shared with the sibling
// internal/parser/golang/symbols package is imported from there rather than
// duplicated.
package dataflow
