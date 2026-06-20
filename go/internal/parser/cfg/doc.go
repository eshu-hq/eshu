// Package cfg builds a per-function control-flow graph and computes reaching
// definitions over it, producing bounded, deterministic def->use facts.
//
// Callers lower a source function into basic blocks, statements, and
// control-flow edges through Builder, then call Build to run the
// reaching-definitions fixpoint and derive block-level control-dependence
// provenance. A statement carries the bindings it defines and uses plus optional
// guard text supplied by a language lowering, so the package is language neutral:
// Go, TypeScript, and Python lowerings share it without the package knowing any
// syntax.
//
// Determinism is a contract. Identical Builder calls always yield a
// byte-identical Function: blocks are emitted in construction order, successors
// and def->use edges are sorted, and statement uses observe the definitions that
// reach the statement entry before the statement's own definitions apply.
//
// Bounds are never silent. Every Limits cap that trips records a counted value
// on Function.Overflow rather than dropping data quietly: past MaxBlocks or
// MaxStmts the fixpoint is skipped (the CFG is still emitted), and past
// MaxDefUseEdges or MaxControlDependencies emission stops in deterministic order
// with the dropped count recorded. Language lowerers that emit field-sensitive
// bindings can also use MaxAccessPathParts and record truncated selector paths on
// Overflow.AccessPaths. Callers that hash the facts for incremental
// recomposition can rely on both the determinism and the explicit overflow
// signal.
package cfg
