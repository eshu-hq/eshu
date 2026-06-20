# Intraprocedural Taint Propagation

## Purpose

`taint` propagates taint within one function over a resolved control-flow graph
and reports flows from sources to sinks, using a kind-set sanitizer model. It is
the second layer of the value-flow taint engine (epic #2705): it consumes the
def->use facts produced by `internal/parser/cfg` and produces findings carrying
confidence and provenance. Cross-function composition is a later pass.

## Ownership boundary

This package owns taint propagation and the kind-set sanitizer semantics. It does
NOT decide what a source, sink, or sanitizer is — a per-language lowering
classifies statements and supplies `Facts`. It does not parse source, build the
CFG, or persist anything; it is a pure function from `(cfg.Function, Facts)` to a
`Result`.

## Exported surface

See `doc.go` for the godoc contract. The surface is:

- `Analyze(fn cfg.Function, facts Facts, limits Limits) Result` — the entry point.
- `Facts`, `SourceMark`, `SanitizerMark`, `SinkMark`, `StmtBinding`, `Kind` — the
  per-statement annotations a lowering supplies.
- `Finding`, `FindingKind` (`FindingTainted`, `FindingSanitized`), `Result`,
  `Limits`, `DefaultLimits` — the output and its bounds.

## Dependencies

- `internal/parser/cfg` for the `cfg.Function` input (blocks + def->use edges).
- Standard library (`sort`). No storage, graph, or telemetry dependencies.

## Telemetry

None. The package is a pure computation; overflow is surfaced as data on the
`Result` for the caller to record, not logged here.

## Gotchas / invariants

- **Kind-set, not binary.** A sanitizer neutralizes specific sink kinds; an HTML
  escaper does not make a value SQL-safe. This is the central correctness rule.
- **Intersection at merges.** When tainted values merge into one definition, the
  neutralized set is the intersection — a kind survives only if every path
  neutralized it. The unsanitized path wins.
- **Monotone fixpoint.** Taint only turns on and neutralized sets only shrink, so
  the worklist terminates. Do not introduce a rule that re-adds a removed kind.
- **Determinism.** Findings are sorted; source/sink/sanitizer facts are map-keyed
  but never iterated into output order. Overflow is counted, never silent.
- **Confidence is fixed per finding kind** for intraprocedural flows (direct, no
  summary composition); the interprocedural pass will use lower confidence.
- **Guard reasons are provenance, not truth upgrades.** When CFG
  control-dependence data is available, findings may include a deterministic
  `GuardReason` chain naming the predicates that gate the sink. The analyzer
  still uses the existing kind-set sanitizer model for the taint verdict.

## Related docs

- Epic #2705 (value-flow taint engine), child issue #2728 (this pass).
- `internal/parser/cfg` (the dataflow substrate), `internal/parser/golang`
  (`cfg_taint_facts.go` — the Go source/sink/sanitizer catalog and lowering).
