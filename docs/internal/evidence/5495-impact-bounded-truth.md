# Impact bounded-truth evidence (#5495)

## What changed

The deployment trace now selects evidence for the authorized service and
workload before applying any public limit. Repository, workload, runtime,
platform, deployment-source, Kubernetes, controller, image, and cloud-resource
identities stay separate through the response and the console graph.

The trace fails closed when it cannot prove completeness. A saturated
limit-plus-one probe reports truncation, missing or contradictory metadata
reports unverified coverage, and a backend failure remains an error. Empty,
incomplete, truncated, unauthorized, and failed reads therefore cannot collapse
into the same complete-looking result.

Cloud candidates enter the canonical graph only when the selected workload has
the required materialized relationship. Configuration and free-text matches
remain bounded candidates with their missing correlation disclosed. ECS and
Kubernetes evidence can coexist without sharing identity, and all returned
nodes and edges use deterministic stable keys and ordering.

## User-visible contract

| User action | Before | After |
| --- | --- | --- |
| Review a deployment chain | A bounded response could still look complete when an upstream family was capped or unverified. | The same response names the affected evidence family and reports truncated or unverified coverage. |
| Inspect cloud evidence | A name or configuration match could appear beside canonical deployment evidence without proving workload correlation. | Uncorrelated matches stay in the candidate section and never become canonical graph edges. |
| Compare ECS and Kubernetes evidence | Similar labels could obscure distinct platform identities. | Canonical IDs and relationship endpoints keep the platform shapes separate. |
| Encounter a graph or content failure | A failed read could be mistaken for an empty deployment chain. | The API and console preserve the error instead of publishing an empty success. |

## Accuracy and edge-state proof

The regression suite covers authorization before selection and limits,
cross-repository and cross-service leakage, workload/repository ambiguity,
uncorrelated cloud candidates, ECS and Kubernetes coexistence, invalid stable
keys, non-finite graph properties, deterministic ordering, exact returned and
observed counts, limit saturation, empty evidence, missing metadata, and
backend errors.

The OpenAPI contract requires the sentinel limit and Kubernetes relationship
completeness fields. The B-7 snapshot and object matcher assert the same fields,
so the handler, wire contract, generated golden evidence, and console
normalization cannot drift independently.

## Performance and observability

This is a correctness change, not a claimed speedup. The selected paths keep
their limit-plus-one probes. Returned evidence families use a 51-row sentinel;
the cloud path separately reads at most 2,501 relationship observations before
resource-identity aggregation. Query-plan coverage and the live NornicDB
profile gate both passed for the emitted production shapes.

The authenticated retained route used 887 authorized repositories and the
PR261 NornicDB image based on v1.1.11. The first exact-branch observation took
3,803 ms for `POST /api/v0/impact/trace-deployment-chain`; after the retained
graph was warm, the same owning request took 2,292 ms. The warm request is
inside the checked-in 2–3 second interactive target. The paired change-surface
request took 6 ms, and the warm route reached response-backed readiness in
4,313 ms. The remaining browser time is presentation and workflow-settle time,
not an unbounded graph read.

No-Observability-Change: the code continues through the shared HTTP, graph, and
content instrumentation. Existing request spans, duration metrics, structured
errors, and in-band truth and limit metadata remain the operator surfaces. The
change adds no write path, worker, queue, retry policy, cache, runtime knob, or
high-cardinality telemetry label.

## Verification

| Proof | Result |
| --- | --- |
| Focused Go query, golden-gate, and golden-corpus tests | Passed |
| Focused Impact console tests | 6 files, 51 tests passed |
| Console typecheck and touched-file lint | Passed |
| Static query-plan tests and generated-coverage verification | Passed |
| Live NornicDB query-plan regression | Passed |
| Replay coverage gate | 413/413 passed; 0 gaps; 0 stale scenarios |
| B-7 live golden corpus | 432 passed; 0 required failures; 0 advisory warnings |
| Authenticated retained `/impact` workflow | Passed with 2 visible `.impact-truth` regions and both required API responses at HTTP 200 |
| `make pre-pr` promotion gate | Passed whole-module format, lint, build, vet, changed-package tests, exactness/telemetry gates, and scoped race tests; generated Go coverage remained 75.4% |

The B-7 run exercised 21 repositories and 17 collectors in 36 seconds:
bootstrap 3 seconds, collection 21 seconds, first drain 7 seconds, maintenance
5 seconds, and graph/query verification 6 seconds.

Both retained observations used API input hash
`a05a158255013f2ed84e5dacc28b79bc6546bd6180fb3d9faafc7f9a51a724b5`.
The cold and warm proof image digests were, respectively,
`sha256:ae078549d577575b5e33ecc77e65c9e2a3cc0d0544d7bc8a36547b039558c7b5`
and
`sha256:af5ad603d1b4e9e619eeb25df4790a20d5f133eccd890bddda71172cfe43d2bf`.
Both used NornicDB image digest
`sha256:8b2207ec7f53836a29c375cf924744fdfeec0e70ec902e66aa22fa0ad648d1bb`.
The rendered Impact page showed the selected `android-github-runner` workload,
the bounded change surface, the deployment narrative, and explicit empty or
incomplete sections without inventing missing relationships.

## Follow-up findings from the retained gate

The selected Impact workflow passed. Two independent baseline/harness defects
also made the wider diagnostic run red and are tracked under the parent epic:

- #5524: Dashboard bootstrap sends a name-only global entity-resolution request
  and receives HTTP 400. The Dashboard caller is unchanged by #5495.
- #5525: a repeated warm route reuses valid same-session React state, but the
  live proof currently requires fresh network traffic for every observation.

Neither failure is hidden or reclassified as a passing full-console gate.
