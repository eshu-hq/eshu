# Deployable-Unit Correlation Contract

Use this contract before turning deployable-unit candidate evaluation into
canonical graph edges. It defines the admission boundary for issue #2505 and
the implementation proof required by the #2452 deployable-unit edge work.

Deployable-unit correlation is reducer-owned graph truth. Collectors and the
ingester may observe repository, file, deployment, workflow, image, or platform
evidence, but only the resolution engine may admit that evidence into canonical
deployable-unit relationships.

## Current State

`DomainDeployableUnitCorrelation` evaluates candidates and publishes the
`deployable_unit_correlation` graph projection phase. Admitted candidates with a
resolved deployment repository now write
`(:Repository)-[:CORRELATES_DEPLOYABLE_UNIT]->(:Repository)` edges through the
shared Cypher edge writer using evidence source
`reducer/deployable-unit-correlation`.

Rejected, ambiguous, low-confidence, stale, or endpoint-less candidates retract
that evidence source for the source repository and write no replacement edge.
Downstream readers may know evaluation ran from the phase, but no query surface
may infer a canonical edge from the phase alone.

## Source Flow

The admissible path is:

```text
repository and file facts
  -> workload candidate extraction
  -> resolved deployment relationship overlay
  -> deployable-unit rule evaluation
  -> admitted or dropped decision
  -> canonical graph edge write
  -> API, MCP, and console readback
```

Every implementation change must preserve that order. Intake services do not
write deployable-unit graph truth, and query handlers do not promote missing
edges into truth from candidate summaries.

## Admission States

Each evaluated deployable-unit candidate must land in exactly one state:

| State | Meaning | Graph effect |
| --- | --- | --- |
| `admitted` | Explicit evidence identifies one deployable unit for one canonical repository or workload scope. | Eligible for one canonical edge family after endpoint readiness passes. |
| `dropped_weak` | Evidence is present but below the rule pack's admission threshold. | No edge. Preserve diagnostics only. |
| `dropped_ambiguous` | Multiple unit keys, repositories, environments, or deployment sources remain plausible. | No edge. Preserve the ambiguity reason. |
| `dropped_unresolved` | A referenced repository, workload, instance, platform, or deployment source cannot be resolved in the active generation. | No edge. Retry only when a required readiness phase is still building. |
| `dropped_stale` | Evidence belongs to a stale generation or superseded source snapshot. | No edge. |
| `deferred_not_ready` | The needed endpoint materialization phase has not committed yet. | No edge in this attempt; return a retryable reducer error. |

An empty candidate set is a successful no-op. It may publish the phase, but it
must not create a placeholder node, synthetic environment, or fabricated edge.

## Endpoint Requirements

Canonical deployable-unit edges must use already-materialized endpoints. A writer
may match `Repository`, `Workload`, `WorkloadInstance`, `Platform`, or
`CloudResource` endpoints that were committed by their owning domains, but it
must not create endpoint nodes as a side effect.

The first canonical edge implementation uses static relationship token
`CORRELATES_DEPLOYABLE_UNIT` and evidence source
`reducer/deployable-unit-correlation` in `go/internal/storage/cypher`. The
relationship identity is the source repository, deployment repository, and
static token; mutable evidence fields are properties, not dynamic relationship
tokens.

Required edge payload fields:

- `scope_id`
- `generation_id`
- source repository id
- deployment repository id when one is part of the proof
- workload id or workload instance id when the endpoint exists
- admission state for admitted rows
- confidence
- method or rule pack name
- bounded reason
- evidence source
- observed source generation
- correlation key

Do not persist raw credentials, private network addresses, provider tokens,
raw cloud account secrets, or provider payload bodies on these edges.

## Drop Invariants

The reducer must drop rather than admit when:

- the only evidence is a repository name, namespace string, folder name, or
  Dockerfile filename
- a deployment repository points at more than one plausible service without an
  exact tie-breaker
- a CI/CD workflow proves a build exists but not the deployable unit it ships
- a deployment relationship references an out-of-scope or unscanned repository
- endpoint materialization has not committed and the missing endpoint is not a
  retryable readiness gap
- the candidate is stale relative to the active scope generation
- duplicate evidence rows disagree on unit key, environment, platform, or source
  repository

Negative and ambiguous states are not failures. They are truth-preserving
outcomes and must remain visible in logs, phase summaries, status rows, or
future read-model diagnostics.

## Read Surface Contract

API, MCP, and console surfaces may expose deployable-unit correlation as exact
only after graph truth exists. A read surface that sees evaluated candidates but
no edge must return missing, building, stale, ambiguous, or unsupported state
instead of presenting a derived relationship.

Readbacks must carry confidence and provenance comparable to other relationship
payloads:

- confidence value
- method or rule pack name
- reason
- evidence source
- freshness or generation state
- next diagnostic status call when freshness is building or stale

## Implementation Gate

Code PRs that implement this contract must prove all layers:

1. Fixture intent: positive, negative, ambiguous, duplicate, stale, and empty
   cases are named in fixtures or focused Go tests.
2. Reducer truth: candidate extraction, admission, endpoint readiness, retry,
   and drop states are tested in `go/internal/reducer`.
3. Graph truth: direct graph assertions prove the concrete edge exists only for
   admitted rows and never for dropped rows.
4. Query truth: API and MCP reads agree with direct graph truth and preserve
   missing or ambiguous states.
5. Performance truth: the writer has a same-shape benchmark or no-regression
   measurement on the target backend with input cardinality, batch size, indexes,
   and queue state recorded in a tracked file.
6. Concurrency truth: duplicate delivery, retry after commit-time conflicts,
   stale generation replay, and parallel reducer workers converge without
   duplicate edges or endpoint creation.
7. Observability truth: logs, spans, metrics, status, or phase summaries let an
   operator identify candidate count, admitted count, drop reasons, write count,
   retry count, and graph-write duration at 3 AM.

Minimum local gates for a future implementation PR:

```bash
cd go && go test ./internal/reducer ./internal/storage/cypher ./internal/query ./internal/mcp -count=1
scripts/test-verify-performance-evidence.sh
scripts/verify-performance-evidence.sh
scripts/test-verify-package-docs.sh
scripts/verify-package-docs.sh
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

Docs-only changes to this contract run:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

Verification evidence must include a targeted sensitive-marker scan over every
changed public doc and navigation file.

## Related Contracts

- [System Architecture](../architecture.md)
- [Collector And Reducer Readiness](collector-reducer-readiness.md)
- [Cypher Performance](cypher-performance.md)
- [Truth Label Protocol](truth-label-protocol.md)
- [Telemetry Overview](telemetry/index.md)
