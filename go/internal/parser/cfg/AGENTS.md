# AGENTS.md - internal/parser/cfg guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract: determinism and bounded-overflow guarantees
3. `cfg.go` - `Builder`, `Block`, `Stmt`, `DefUse`, `Function`, and the
   construction API
4. `reaching.go` - the reaching-definitions fixpoint, transfer function, and
   deterministic def->use emission
5. `limits.go` - the caps and their normalization
6. `reaching_test.go` - the straight-line, branch-merge, loop back-edge,
   determinism, and overflow proofs
7. The Go lowering that feeds this package: `../golang/cfg_lower.go`,
   `../golang/cfg_bindings.go`, `../golang/cfg_emit.go`

## Invariants this package enforces

- Language neutral: this package must not import any language grammar,
  tree-sitter binding, parser, storage, or telemetry package. Standard library
  only.
- Determinism is a contract. Identical Builder calls yield a byte-identical
  Function. Never let map iteration order leak into emitted output; sort first.
- A use observes the reaching set at the statement entry, before that
  statement's own definitions apply.
- Parameters are modeled by lowerings as definitions in the entry block; the
  entry block's in-set is empty.
- Bounds are counted, never silent. A tripped cap records a count on
  `Function.Overflow`; emission never drops data without that signal.

## Common changes and how to scope them

- Change reaching-definition semantics: add or update a focused case in
  `reaching_test.go` first (it drives the algorithm directly through Builder,
  with no parser dependency), then edit `reaching.go`.
- Add a new bound: add the cap to `Limits`, normalize it in `limits.go`, add a
  counted field to `Overflow`, and prove the truncation with a test.
- Add a new fact field on `DefUse` or `Block`: update `cfg.go`, keep the sort
  keys total and deterministic, and assert the order in a test.

## Failure modes and how to debug

- Non-deterministic output: a map is being iterated into emitted order. Find the
  unsorted producer; every emitted slice must have a total sort order.
- Missing def->use edge: usually the lowering did not record the def or use, or
  a control-flow edge is absent. Dump `Function.Blocks` (id, succs, per-stmt
  defs/uses) before suspecting the fixpoint.
- Fixpoint never terminates: the transfer function is non-monotone (it must only
  add or kill within the finite def-id lattice). Reaching definitions are
  monotone by construction; check for accidental unbounded growth.

## Do not change without review

- The determinism contract or the overflow-is-counted rule. Downstream passes
  (incremental summaries, #2729) hash these facts; silent nondeterminism or
  silent truncation corrupts that.
