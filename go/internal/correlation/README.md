# Correlation

## Purpose

`correlation` exposes lightweight reporting helpers over completed correlation
evaluations and anchors the sub-package split for model, rules, engine,
admission, and explain behavior.

## Ownership boundary

The root package owns `Summary` and `BuildSummary`. It does not perform rule
evaluation, admission, tie-breaking, explain rendering, queue work, graph
writes, or materialization. Reducer callers consume engine evaluations directly
for projection input.

## Exported surface

Use `doc.go` and `go doc ./internal/correlation` for the godoc contract.
`BuildSummary` reduces one `engine.Evaluation` into evaluated-rule, admitted,
rejected, conflict, and low-confidence counters.

## Dependencies

The root package depends on `correlation/engine` for evaluation shape and
`correlation/model` for candidate states and rejection reasons.

## Telemetry

None. `BuildSummary` returns plain Go data. Callers attach reducer status,
structured logs, metrics, or trace attributes around the summary they receive.

## Gotchas / invariants

- `Summary.EvaluatedRules` comes from `engine.Evaluation.OrderedRuleNames`, not
  the raw rule-pack length.
- One rejected candidate can increment both conflict and low-confidence counters
  when it carries both rejection reasons.
- Provisional candidates are skipped by the admitted/rejected counters. A final
  evaluation containing provisional state is a pipeline bug upstream.
- Do not merge conflict and low-confidence counters; operators need to know
  whether tie-breaks or confidence gates rejected candidates.

## Related docs

- `go/internal/correlation/model/README.md`
- `go/internal/correlation/rules/README.md`
- `go/internal/correlation/engine/README.md`
- `go/internal/correlation/admission/README.md`
- `go/internal/correlation/explain/README.md`
