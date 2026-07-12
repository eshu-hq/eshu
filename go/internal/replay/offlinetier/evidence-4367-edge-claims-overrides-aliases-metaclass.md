# Evidence: live retract claims for OVERRIDES, ALIASES, USES_METACLASS (#4367)

Coverage-claim change only: no production write or retract code changes. The
three edge types ride retract fixes that already merged — OVERRIDES/ALIASES
through the per-child-label inheritance retract (#5120, rel-type disjunction
INHERITS|OVERRIDES|ALIASES|IMPLEMENTS), USES_METACLASS through the
per-source-label code-call retract (#5116) under the parser/python-metaclass
evidence source — but the replay-coverage gate only checks that a referenced
artifact exists, so claiming them without a live test that actually writes and
retracts each type would be a false green. This change adds that proof:

- `TestReducerInheritanceEdgeRetractGraphTruth` now writes and retracts
  OVERRIDES (Class->Class, matching the reducer's class-level trait-override
  emission) and ALIASES (Function->Function, matching its method-level
  emission) alongside INHERITS and IMPLEMENTS, covering both child shapes
  production writes.
- `TestReducerMetaclassEdgeRetractGraphTruth` writes and retracts a
  Class->Class USES_METACLASS edge through the production
  `EdgeWriter.WriteEdges`/`RetractEdges` paths. Its write template is the
  UNWIND + label-disjunction + inline `{uid}` anchor shape, which matches
  correctly on v1.1.11 (probed in
  `go/internal/storage/cypher/evidence-4367-rationale-delta-retract-per-label.md`).

## No-Regression Evidence:

No production code changed; the touched surfaces are two live tests, the
coverage manifest, the replay-tier run list, and the regenerated coverage
dashboard/report. Live proof on the pinned
`timothyswt/nornicdb-cpu-bge:v1.1.11`:

```bash
ESHU_REPLAY_TIER_LIVE=1 ESHU_GRAPH_BACKEND=nornicdb ESHU_NEO4J_DATABASE=nornic \
NEO4J_URI=bolt://localhost:17689 \
go test ./internal/replay/offlinetier/ -run 'TestReducerInheritanceEdgeRetractGraphTruth|TestReducerMetaclassEdgeRetractGraphTruth' -count=1
```

ok 3/3 runs (~1.0–1.5s package wall): every in-scope edge of all three claimed
types retracts to zero, out-of-scope edges survive, and every endpoint node
survives. Full `bash scripts/verify-replay-tier.sh` PASSED on the final tree
with the metaclass test added to the gate's run list.

## Observability Evidence:

No-Observability-Change. No metric, span, log field, queue stage, worker knob,
or status field changes; the tests observe the existing canonical write and
retract paths.
