// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package valueflow is the semantic bridge that composes the four value-flow
// engines into an end-to-end interprocedural taint pipeline: it derives a
// function's summary effects from its control-flow graph and intraprocedural
// taint facts (internal/parser/cfg, internal/parser/taint), feeds them to the
// incremental summary store (internal/parser/summary), and assembles an
// interprocedural port graph (internal/parser/interproc) from the summaries.
//
// DeriveEffects reuses the intraprocedural taint engine instead of a second
// propagation: it treats each parameter and internal source as a taint origin
// and each return statement and call-argument site as a pseudo-sink, then reads
// the resulting flows as the TITO summary (param->sink, param->return,
// param->call-arg, source->return). One propagation implementation and one
// kind-set sanitizer model serve the whole engine; a parameter sanitized before
// a sink simply produces no param->sink effect.
//
// BuildProgram turns the per-function summaries plus externally-known sources
// and sinks (an HTTP request parameter, a correlated cloud fact) into an
// interproc.Program: param->call-arg effects become cross-function edges,
// param->sink effects become sinks at the parameter port, and param->return
// effects become edges to the return port.
//
// The package is language neutral: a per-language lowering supplies an
// EffectsSpec (which statements are parameters, sources, sinks, sanitizers,
// returns, and call-argument sites) mapped onto the control-flow graph. The
// Go-AST-to-EffectsSpec extraction (call resolution and argument binding) is the
// remaining integration step.
package valueflow
