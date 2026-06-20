# internal/queryplan

`internal/queryplan` validates the static query-plan regression manifest for
hot graph read paths. It checks that each registered Cypher path declares its
source owner, selective anchors, schema evidence, bounds, ordering, and optional
backend plan-operator fixtures.

The package does not connect to graph backends. It is a deterministic CI gate
for query shape regressions and backend caveat drift; live `EXPLAIN` or
`PROFILE` capture remains separate runtime evidence.

## Evidence Contract

- Cypher entries must be anchored on declared label/property pairs.
- Variable-length traversals must have an upper bound.
- Paginated paths using `SKIP` must order before offsetting.
- Required schema names must exist in the supplied schema statement list.
- SQL/read-model entries must carry caveats so the gate does not pretend they
  have graph plans.

No-Regression Evidence: `go test ./internal/queryplan -count=1` exercises the
accepted hot-path shape and deliberately bad query/plan fixtures.

No-Observability-Change: this package performs static validation only. It adds
no API route, graph query, graph write, metric, span, runtime knob, queue work,
or provider call.
