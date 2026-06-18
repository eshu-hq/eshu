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
// edge. Nested function definitions and lambdas are not descended into; closures
// are modeled by a later pass. Parameters are modeled as definitions in the entry
// block so value flow from a parameter into the body is captured.
//
// For an attribute access (a.b) only the object (a) is a use; the attribute name
// is not a variable. Tuple and list assignment targets define each of their
// identifiers. The result is bounded and deterministic via the cfg engine.
//
// TaintFacts derives intraprocedural taint annotations (sources, sinks,
// sanitizers) for a function from a small, conservative Python catalog mapped
// onto the control-flow graph, ready for the internal/parser/taint engine.
// Sources require framework request type evidence; sinks require qualified
// receiver/module evidence except for Python builtins such as eval and exec.
// Sanitizers remain direct-call only.
//
// EffectsSpec, LocalFunctionIDs, and FunctionID build a function's value-flow
// summary spec for cross-function composition; InterprocFindings composes the
// per-function summaries of a file into an interprocedural port graph and solves
// it, returning the cross-function taint findings. Resolution is intra-file:
// only top-level functions are entries and only bare-identifier calls resolve to
// a local callee, so a method call or a nested (lexically private) function never
// invents a false cross-function edge.
package pydataflow
