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
- Every referenced deployment dimension must be single-valued on the
  canonical finding. One finding has one suppression decision, so a `stage`
  predicate cannot hide a prod+stage aggregate. Multi-dimension scopes also
  require verified tuple evidence where the reducer has a real join.

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

`TestEvaluateSupplyChainSuppressionEnvironmentScopeKeepsMultiEnvironmentAggregateVisible`
builds the finding through `finalizeSupplyChainImpactFinding` with real
prod+stage deployment contexts, then proves the stage-scoped suppression
leaves the aggregate visible as `scope_mismatch`. Singleton stage evidence
still suppresses. This preserves stable vulnerability identity and prevents a
context predicate from hiding sibling exposures.

`TestEvaluateSupplyChainSuppressionDeploymentContextDoesNotCrossVulnerabilityIdentity`
proves that shared `prod` context never makes a CVE-A suppression adjacent to
CVE-B. Deployment fields narrow an already-matching vulnerability identity;
they never attach another vulnerability's suppression ID or mismatch state.

## Prove-the-theory-first performance work

The rejected design made a common environment a SQL discovery key. On the
same 300,000-row synthetic Postgres corpus used during the branch
investigation, the `prod` shape matched 85,715 rows and the real paginated Go
loader took about 22.7 seconds. Removing deployment-only discovery eliminates
that scan class. The existing 2,000-row per-call cap remains as fail-open
defense in depth for unexpectedly broad identity filters: when its sentinel
proves the suppression candidate set is incomplete, the handler discards the
entire retained suppression prefix before evaluation. It therefore keeps the
finding active instead of persisting an older retained assertion when the
globally preferred AuthoredAt/ID winner may be beyond the cap.

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
| Mean ns/op | 9,106 | 3,656 | 3,750 |
| Range ns/op | 9,059–9,144 | 3,637–3,692 | 3,738–3,767 |
| B/op | 43,964 | 112 | 112 |
| allocs/op | 14 | 4 | 4 |

The legacy shape is about 60% faster than current main with 10 fewer
allocations. The deployment shape performs the three new conjunct checks and
is still about 59% faster than current main with 10 fewer allocations. Its
setup includes the genuine `(service-0, workload-0)` pair and asserts the
expected `not_affected` decision before the timer starts, so these numbers
measure a successful deployment-scoped match rather than the cheaper
scope-mismatch path. Accuracy is unchanged by the selection rewrite; the
differential proof above pins exact decision equivalence.

The final real-Postgres proof seeded 100,000 active facts and took ten warm
measurements against the exact query from base commit
`37649ca04ad51408494af96955876a8c8ad49f22` and the current shipped query on
the same isolated schema. The committed base fixture is byte-for-byte the
query constant at that commit and is hash-guarded in the test.

For the output-equivalent CVE filter, base and current both returned the same
ordered 250 rows. Base median/p95 was 14.783/15.369 ms; current median/p95 was
12.855/14.421 ms, improving both boundaries and satisfying the enforced 10%
no-regression ceiling.

For advisory normalization, the corpus deliberately gives 125 suppression
rows a mixed-case advisory ID padded with Unicode non-breaking spaces.
The exact base predicate returned only the 125 top-level exact matches.
The current query returned all 250 intended advisory rows at
12.980/13.598 ms median/p95. This is an explicit correctness delta rather
than an output-equivalent speed comparison: #5466 replaces #5465's exact
nested advisory comparison with the same case-folding and Unicode
`strings.TrimSpace` semantics used by the reducer. The row-cap integration
proof also passed without truncating core evidence or overstating exact-cap
truncation.

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

# exact-base versus current SQL, same 100,000-row isolated Postgres schema
ESHU_POSTGRES_TEST_DSN='postgresql://eshu:change-me@localhost:25432/eshu?sslmode=disable' \
  GOCACHE=$PWD/../.gocache go test -tags=integration \
  ./internal/storage/postgres \
  -run 'TestSupplyChainImpactAdvisoryFilterPlanLive|TestListActiveSupplyChainImpactFactsNormalizesAdvisoryScopeLive' \
  -count=1 -v
```

## Live golden proof

The golden suppression is identity-anchored by CVE, repository, and digest,
then narrowed to `environment=prod`. The live finding carries baked
`prod`/`deploy_event` evidence. The gate's hidden-then-expired assertions
therefore prove the environment conjunct on the real producer, Postgres,
queue, reducer, and query path: an inert or mismatched conjunct would produce
`scope_mismatch`, not the asserted hidden and expired states.

The exact-head live gate passed with 510 checks, 0 required failures, and two
advisory timing warnings. The graph-query check completed in 22 seconds against
its 20-second advisory ceiling, and the maintenance drains completed in 20
seconds against their 19-second advisory ceiling. Total wall time was 110
seconds, below the 30-minute required ceiling. Every suppression drain
reported zero residual and zero dead-letter work.

Measured suppression-path timings:

| Operation | Wall or p50 time |
| --- | ---: |
| Scope setup mutation | 0.007517 s |
| Scope setup drain | 2.099366 s |
| Malformed-input drain | 2.108445 s |
| Active baseline query p50 | 0.019233 s |
| Ignore mutation | 0.005002 s |
| Ignore drain | 2.105417 s |
| Hidden query p50 | 0.006667 s |
| Audit query p50 | 0.010262 s |
| Identical retry mutation p50 | 0.003371 s |
| Expired-visible query p50 | 0.014130 s |

Command:

```bash
docker compose -f docker-compose.yaml down -v
GOCACHE=$PWD/.gocache ESHU_POSTGRES_PORT=25432 \
  NEO4J_HTTP_PORT=27474 NEO4J_BOLT_PORT=27687 \
  bash scripts/verify-golden-corpus-gate.sh
```
