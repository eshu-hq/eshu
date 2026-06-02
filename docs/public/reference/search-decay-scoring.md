# Search Decay Scoring

Search decay scoring is ranking metadata for selected non-canonical evidence. It
is not canonical graph truth, an API/MCP response contract, or a default runtime
search feature.

The contract lives in `go/internal/searchdecay`. It gives issue #418 a bounded
policy and observation shape before live retrieval adapters or telemetry
exporters use decay scores.

## Eligible Evidence

The default eligible evidence classes are:

| Evidence class | Meaning |
| --- | --- |
| `ci_run` | CI run evidence. |
| `vulnerability_observation` | Vulnerability observation evidence. |
| `deployment_event` | Deployment event evidence. |
| `cloud_observation` | Live cloud observation evidence. |
| `relationship_candidate` | Weak inferred relationship candidates. |

Decay is skipped for:

- canonical graph evidence;
- admitted durable relationships;
- evidence with a non-derived truth level;
- evidence classes outside the active policy.

## Policy Contract

Each policy includes:

| Field | Meaning |
| --- | --- |
| `id` | Stable low-cardinality policy id. |
| `now` | Clock used for deterministic scoring. |
| `half_life` | Duration that halves score contribution. |
| `min_score` | Lower bound after decay. |
| `eligible_classes` | Optional override for eligible evidence classes. |

Scores are bounded from `0` to `1`. Invalid policies or evidence are rejected
before scoring, including missing truth labels.

## Decision Contract

Each scoring call returns a decision with:

- policy id;
- evidence class;
- outcome;
- original score;
- final score;
- evidence age;
- reason.

Supported outcomes are:

| Outcome | Meaning |
| --- | --- |
| `applied` | Decay adjusted eligible non-canonical evidence. |
| `skipped_canonical` | Canonical or durable evidence was not decayed. |
| `skipped_ineligible` | Evidence class was outside policy scope. |
| `rejected_invalid` | Policy or evidence validation failed. |

## Observation Contract

`Scorer` emits one optional observation per scoring attempt. Live callers must
bridge observations to operator-facing counts by:

- policy id;
- evidence class;
- outcome.

Do not use evidence ids, graph handles, repository ids, service ids, or other
high-cardinality values as metric labels.

## Verification Gate

Focused package gate:

```bash
cd go && go test ./internal/searchdecay ./internal/searchdocs -count=1
```

Docs changes must also pass:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

## Related Docs

- [Search Benchmark Evidence](search-benchmark-evidence.md)
- [Search Document Projection](search-document-projection.md)
- [Truth Label Protocol](truth-label-protocol.md)
