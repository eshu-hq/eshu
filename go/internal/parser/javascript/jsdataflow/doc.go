// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package jsdataflow lowers a TypeScript or JavaScript function into a
// control-flow graph and resolves reaching definitions over it, reusing the
// language-neutral internal/parser/cfg engine. It is the TS/JS counterpart of the
// Go lowering and the first step of TS/JS value-flow taint (epic #2705, issue
// #2826).
//
// Control flow is lowered precisely for blocks, if/else, for, for-in/of, and
// while; constructs not modeled precisely yet contribute their identifier uses
// but no definitions, which can miss a reaching definition but never invents a
// false edge. Parameters, including object and array destructuring patterns, are
// modeled as definitions in the entry block so value flow from a parameter into
// the body is captured.
//
// Bindings are field-sensitive (accesspaths.go), mirroring the Go template: a
// member target obj.field defines the access path obj.field, a subscript m[k]
// lowers to the explicitly labeled whole-container approximation m[*], and a
// field write through a reference alias (let a = obj; a.field = x) normalizes to
// the aliased object. Paths deeper than cfg.Limits.MaxAccessPathParts truncate to
// a "*"-suffixed prefix and count Overflow.AccessPaths, never a silent drop. A
// function literal passed as a call argument is descended into to attribute its
// captured (free) variables to the enclosing function, excluding the closure's
// own parameters and inner-scope definitions; destructured closure parameters
// and locals shadow outer names. A non-invoked literal is not.
//
// The result is bounded and deterministic: the cfg engine sorts its output and
// records counted overflow rather than dropping data silently.
//
// TaintFacts derives intraprocedural taint annotations (sources, sinks,
// sanitizers) for a function from a small, conservative TS/JS catalog mapped onto
// the control-flow graph, ready for the internal/parser/taint engine. Sources
// require framework request type evidence from qualified annotations or
// framework imports; sinks require a qualified receiver/module except for
// language builtins such as eval.
package jsdataflow
