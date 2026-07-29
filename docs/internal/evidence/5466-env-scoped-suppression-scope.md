# #5466 environment-scoped suppression evidence

## Contract and accuracy

`environment`, `workload_id`, and `service_id` are optional conjuncts on a
vulnerability suppression. They never identify a vulnerability by themselves.
Every admitted suppression still requires at least one discoverable identity
anchor: `cve_id`, `advisory_id`, `package_id`, `purl`, `repository_id`, or
`subject_digest`.

This boundary matters for both accuracy and performance:

- Deployment-only input is rejected by the authenticated producer and
  quarantined as `input_invalid` at the reducer decoder.
- A directly constructed invalid deployment-only scope fails closed to
  `scope_mismatch`; it cannot hide unrelated findings.
- Postgres discovers suppression facts through identity anchors only.
  Deployment context is checked after the anchored fact is loaded.
- Environment values use the shared alias canonicalizer.
- A multi-dimension deployment scope fails closed when flattened finding
  evidence cannot prove one correlated deployment tuple.

The behavior change is versioned as the additive, backward-compatible
`vulnerability.suppression` schema `1.1.0`. A compatibility test decodes a
`1.0.0` payload through the same major-1 reader with all new fields absent.

Failing-then-green proofs:

```text
TestEvaluateSupplyChainSuppressionEnvironmentOnlyScopeFailsClosed
TestEvaluateSupplyChainSuppressionSpecificScopeOutranksInvalidEnvironmentOnly
TestBuildVulnerabilitySuppressionsQuarantinesDeploymentContextWithoutIdentityAnchor
TestCreateVulnerabilitySuppressionValidatesAndPersistsOperatorFact
TestListActiveSupplyChainImpactFactsQueryDoesNotDiscoverSuppressionByDeploymentContext
```

The acceptance fixture
`TestEvaluateSupplyChainSuppressionSameDigestTwoEnvironmentsHidesOnlyScopedEnvironment`
uses two findings with the same digest. The `stage` finding is suppressed and
the `prod` finding remains visible as `scope_mismatch`.

## Prove-the-theory-first performance work

The rejected design made a common environment a SQL discovery key. On the
same 300,000-row synthetic Postgres corpus used during the branch
investigation, the `prod` shape matched 85,715 rows and the real paginated Go
loader took about 22.7 seconds. Removing deployment-only discovery eliminates
that scan class. The existing 2,000-row per-call cap remains as fail-open
defense in depth for unexpectedly broad identity filters.

Adding three strings widened each suppression value. The first benchmark
against current `origin/main` exposed a legacy-shape regression: current main
averaged 9,106 ns/op and 43,964 B/op, while the pre-fix branch ranged from
12,492 to 14,629 ns/op at 49,726 B/op. That regression blocked promotion.

A test-only online-selection shim then proved that tracking the best candidate
per precedence class could replace four append-and-stable-sort buckets:
3,583–3,761 ns/op, 112 B/op, and 4 allocs/op on the same input. Production
code was changed only after that measurement. The differential test
`TestEvaluateSupplyChainSuppressionOnlineSelectionMatchesStableSort` compares
the finished implementation with the former stable-sort algorithm across
active, provider, expired, mismatch, timestamp-tie, ID-tie, and precedence
cases.

Final measurements, Apple M5 Max, `darwin/arm64`, five one-second samples:

| Metric | Current main | #5466 legacy shape | #5466 deployment shape |
| --- | ---: | ---: | ---: |
| Mean ns/op | 9,106 | 3,738 | 3,980 |
| Range ns/op | 9,059–9,144 | 3,658–3,770 | 3,950–4,059 |
| B/op | 43,964 | 112 | 656–657 |
| allocs/op | 14 | 4 | 5 |

The legacy shape is about 59% faster than current main with 10 fewer
allocations. The deployment shape performs the three new conjunct checks and
is still about 56% faster than current main. Accuracy is unchanged by the
selection rewrite; the differential proof above pins exact decision
equivalence.

The final real-Postgres proof seeded 100,000 active facts and took ten warm
measurements. The legacy query median/p95 was 15.456/16.515 ms; the current
identity-anchored query was 15.307/16.997 ms, within the enforced 10% ceiling
at both boundaries and with an identical 250-row CVE result set. The new
advisory-only identity path returned exactly its intended 250 rows at
16.558/18.151 ms median/p95. The row-cap integration proof also passed without
truncating core evidence.

Commands:

```bash
# current origin/main 37649ca04a, throwaway worktree
GOCACHE=$PWD/../.gocache go test ./internal/reducer -run '^$' \
  -bench '^BenchmarkEvaluateSupplyChainSuppression5466Baseline$' \
  -benchmem -benchtime=1s -count=3

# finished branch
GOCACHE=$PWD/../.gocache go test ./internal/reducer -run '^$' \
  -bench 'BenchmarkEvaluateSupplyChainSuppression_(LegacyScopeOnly|WithEnvironmentWorkloadServiceScope)$' \
  -benchmem -benchtime=1s -count=5

GOCACHE=$PWD/../.gocache go test ./internal/reducer \
  -run 'OnlineSelectionMatchesStableSort|Suppression' -count=1
```

## Live golden proof

The golden suppression is identity-anchored by CVE, repository, and digest,
then narrowed to `environment=prod`. The live finding carries baked
`prod`/`deploy_event` evidence. The gate's hidden-then-expired assertions
therefore prove the environment conjunct on the real producer, Postgres,
queue, reducer, and query path: an inert or mismatched conjunct would produce
`scope_mismatch`, not the asserted hidden and expired states.

The final live gate passed with 511 checks, 0 required failures, and one
advisory graph-query timing warning. Total wall time was 106 seconds, below the
30-minute required ceiling. Every suppression drain reported zero residual and
zero dead-letter work.

Measured suppression-path timings:

| Operation | Wall or p50 time |
| --- | ---: |
| Scope setup mutation | 0.007588 s |
| Scope setup drain | 2.079212 s |
| Malformed-input drain | 2.096132 s |
| Active baseline query p50 | 0.019466 s |
| Ignore mutation | 0.004468 s |
| Ignore drain | 2.092349 s |
| Hidden query p50 | 0.007173 s |
| Audit query p50 | 0.008477 s |
| Identical retry mutation p50 | 0.003126 s |
| Expired-visible query p50 | 0.008204 s |

Command:

```bash
docker compose -f docker-compose.yaml down -v
GOCACHE=$PWD/.gocache ESHU_POSTGRES_PORT=25432 \
  NEO4J_HTTP_PORT=27474 NEO4J_BOLT_PORT=27687 \
  bash scripts/verify-golden-corpus-gate.sh
```
