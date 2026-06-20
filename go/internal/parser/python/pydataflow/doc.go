// Package pydataflow lowers a Python function into a control-flow graph and
// resolves reaching definitions over it, reusing the language-neutral
// internal/parser/cfg engine. It is the Python counterpart of the Go and TS/JS
// lowerings and a step toward Python value-flow taint (epic #2705, issue #2826).
//
// Control flow is lowered precisely for blocks, if/elif/else, for-in, while,
// with, and try/except (its handlers branch from the pre-try state, a
// conservative over-approximation); constructs not modeled precisely yet
// contribute their identifier uses but no
// definitions, which can miss a reaching definition but never invents a false
// edge. Parameters are modeled as definitions in the entry block so value flow
// from a parameter into the body is captured.
//
// Bindings are field-sensitive (accesspaths.go), mirroring the Go template: an
// attribute target obj.attr defines the access path obj.attr (and an attribute
// read records obj.attr plus the base object obj); a subscript d[k] lowers to the
// explicitly labeled whole-container approximation d[*]; and an attribute write
// through a reference alias (a = obj; a.attr = x) normalizes to the aliased
// object. Paths deeper than cfg.Limits.MaxAccessPathParts truncate to a
// "*"-suffixed prefix and count Overflow.AccessPaths, never a silent drop. Only
// the base segment of a multi-part path is alias-resolved, so a bare identifier
// read keeps its reaching-def identity. A lambda passed as a call argument is
// descended into to attribute its captured (free) variables to the enclosing
// function, excluding its own parameters and inner-scope assignments; a
// non-invoked lambda or a nested def is not. Tuple and list assignment targets
// define each of their identifiers. The result is bounded and deterministic via
// the cfg engine.
//
// TaintFacts derives intraprocedural taint annotations (sources, sinks,
// sanitizers) for a function from a small, conservative Python catalog mapped
// onto the control-flow graph, ready for the internal/parser/taint engine.
// Sources require framework request type evidence from qualified annotations or
// framework imports; sinks require qualified receiver/module evidence except for
// Python builtins such as eval and exec. Sanitizers remain direct-call only.
//
// EffectsSpec, LocalFunctionIDs, and FunctionID build a function's value-flow
// summary spec for cross-function composition; InterprocFindings composes the
// per-function summaries of a file into an interprocedural port graph and solves
// it, returning the cross-function taint findings. Resolution is intra-file:
// only top-level functions are entries and only bare-identifier calls resolve to
// a local callee, so a method call or a nested (lexically private) function never
// invents a false cross-function edge.
package pydataflow
