// Package jsdataflow lowers a TypeScript or JavaScript function into a
// control-flow graph and resolves reaching definitions over it, reusing the
// language-neutral internal/parser/cfg engine. It is the TS/JS counterpart of the
// Go lowering and the first step of TS/JS value-flow taint (epic #2705, issue
// #2826).
//
// Control flow is lowered precisely for blocks, if/else, for, for-in/of, and
// while; constructs not modeled precisely yet contribute their identifier uses
// but no definitions, which can miss a reaching definition but never invents a
// false edge. Nested function and arrow-function bodies are not descended into;
// closures are modeled by a later pass. Parameters are modeled as definitions in
// the entry block so value flow from a parameter into the body is captured.
//
// The result is bounded and deterministic: the cfg engine sorts its output and
// records counted overflow rather than dropping data silently.
//
// TaintFacts derives intraprocedural taint annotations (sources, sinks,
// sanitizers) for a function from a small, conservative TS/JS catalog mapped onto
// the control-flow graph, ready for the internal/parser/taint engine. Sources
// require framework request type evidence; sinks require a qualified
// receiver/module except for language builtins such as eval.
package jsdataflow
