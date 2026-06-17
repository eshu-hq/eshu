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
// Sources are Django/Flask-style request parameters; sinks and sanitizers are
// recognized calls (cursor.execute, os.system, html.escape).
package pydataflow
