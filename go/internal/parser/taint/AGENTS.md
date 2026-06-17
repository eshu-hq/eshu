# AGENTS.md - internal/parser/taint guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract: kind-set model and determinism guarantees
3. `taint.go` - `Analyze`, the `Facts`/`Finding` types, confidence constants
4. `propagate.go` - the value-flow graph, the monotone fixpoint, sink evaluation
5. `taint_test.go` - source->sink, correct/wrong-kind sanitizer, intersection,
   determinism, and overflow proofs
6. The Go lowering that supplies facts: `../golang/cfg_taint_facts.go`

## Invariants this package enforces

- Language neutral: this package must not import any tree-sitter binding,
  parser, storage, graph, or telemetry package. It depends only on
  `internal/parser/cfg` and the standard library.
- Kind-set sanitizer model: a sanitizer neutralizes specific sink kinds. A sink
  of kind K fires unless K is neutralized on every path reaching it. Never
  collapse this to a binary "sanitized" flag.
- Intersection at merges: merging tainted paths intersects their neutralized
  sets; the unsanitized path wins.
- Monotone fixpoint: taint only turns on, neutralized sets only shrink. Do not
  add a rule that re-taints a clean value or re-adds a removed neutralized kind.
- Deterministic, bounded output: findings sorted; map-keyed facts never iterated
  into output order; overflow counted, never silent.

## Common changes and how to scope them

- Change kind-set semantics: add or update a case in `taint_test.go` first (it
  drives `Analyze` directly with hand-built CFG + Facts), then edit
  `propagate.go`.
- Add a new finding field: update `Finding` in `taint.go`, keep the sort keys
  total and deterministic, and assert order in a test.
- Tune confidence: change the constants in `taint.go`; keep intraprocedural
  confidence above whatever the interprocedural pass uses.

## Failure modes and how to debug

- Wrong-kind sanitizer suppresses a sink: the neutralized set is being applied
  without the kind check. Verify the sink evaluation tests the exact sink kind
  against the neutralized set.
- Non-deterministic findings: a map (facts.Sinks, facts.Sources) is iterated
  into output order. Sort the keys first, as `evaluateSinks` and the source seed
  loop do.
- Fixpoint never terminates: a non-monotone update (re-adding a removed kind or
  re-tainting). The lattice only allows taint on and neutralized sets shrinking.

## Do not change without review

- The kind-set model or the intersection-at-merge rule. They are the correctness
  contract that makes this more precise than a binary sanitizer; later passes and
  findings consumers depend on it.
