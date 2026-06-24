// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package taint runs intraprocedural taint propagation over a resolved
// control-flow graph using a kind-set sanitizer model, producing bounded,
// deterministic findings with confidence and provenance.
//
// Callers supply a cfg.Function and Facts: which definitions are sources, which
// statements sanitize (neutralize specific sink kinds), and which statements are
// sinks of a given kind. Analyze flows taint along def->use chains. Each tainted
// value carries an accumulated set of neutralized sink kinds; when two tainted
// paths merge into one definition the sets are intersected, so a kind is treated
// as neutralized only if every path neutralized it. A sink of kind K reports
// TAINTED unless K is neutralized on every path reaching it, in which case it
// reports SANITIZES.
//
// The kind set is strictly more precise than a binary sanitizer: an HTML escaper
// does not neutralize a SQL sink. The analysis is monotone (taint only turns on;
// neutralized sets only shrink) so the fixpoint terminates. Output is sorted and
// the finding count is bounded with a counted overflow, never a silent drop.
//
// The package is language neutral: a per-language lowering classifies sources,
// sinks, and sanitizers and maps them onto the control-flow graph.
package taint
