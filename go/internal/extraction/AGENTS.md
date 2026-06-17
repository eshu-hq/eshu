# AGENTS: extraction

Scoped rules for editing the advisory extraction-readiness engine.

## Read first

- `doc.go`, `README.md` here.
- `docs/public/reference/collector-extraction-policy.md` — the single source of
  truth for criteria and family membership. Keep this package in lockstep with it.

## Invariants

- This package is **advisory**. Never make it move code, disable a collector,
  mutate a manifest, write facts, touch the graph, or change runtime behavior.
- `Evaluate` MUST stay deterministic and total: same `Profile` in, same
  `Readiness` out, with missing criteria failing closed as `unmet`. Do not add
  time, randomness, environment, or I/O.
- The catalog MUST stay honest. Do not mark a family `external_ready` or move a
  criterion to `met` without real, citable repo evidence (a landed proof, test,
  or script). Over-claiming here misleads contributors about what is safe to
  extract.
- Criteria are the seven policy rows in `orderedCriteria`. If the policy table
  changes, change `orderedCriteria` and every `Profile` together, then update the
  policy doc in the same PR.

## Common changes

- A family completes its out-of-tree proof: flip its `Profile` criteria/flags in
  `catalog.go`, update the expected classification in `catalog_test.go`, and cite
  the proof in the profile `Detail`/`Rationale`.
- Add a tracked family: add a `Profile` whose `Family` is a real
  `scope.CollectorKind`, ensure `Validate` passes (all seven criteria once), and
  add it to the policy doc's keep-in-tree or candidate list.

## Verification

```bash
cd go && go test ./internal/extraction ./cmd/eshu -count=1
cd go && gofmt -l internal/extraction cmd/eshu
```

No performance or observability markers are required: this package adds no
Cypher, graph write, worker, queue, lease, batching, or runtime stage. It is
pure in-process classification with no telemetry surface.
