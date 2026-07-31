# #5740 Container Image Identity Canonicalization Evidence

## Contract

Container image identity is canonical by immutable digest. Independent source,
registry, build, deployment, and provenance observations remain separate typed
support rows under that digest. Each scope publishes one complete immutable
support set and atomically selects it through
`container_image_identity_scope_state.active_set_id`.

Publication requires the exact reducer work item, claim epoch, scope,
generation, activation epoch, and an `active` generation status. Generation
activation clears the pointer and increments the epoch. Before the first v3
publication only active-generation legacy facts are readable; the first v3
publication carries exact warning-held supports, removes those legacy rows,
and becomes the sole authority.

## Accuracy and lifecycle proof

The implementation was developed from failing regressions for these cases:

- a warning-held typed support was dropped by complete-set replacement;
- a non-active generation could load prior support and publish a set;
- legacy cleanup was performed but reported as zero.
- digest-level presentation folding erased the support-level repository
  correlation used by supply-chain impact, CI/CD, and SBOM reducers;
- a global union cursor from another fact kind was decoded as a support cursor.

The focused live proof is:

```bash
ESHU_POSTGRES_TEST_DSN='postgresql://eshu:change-me@127.0.0.1:25432/eshu?sslmode=disable' \
  go test ./internal/reducer ./internal/storage/postgres \
  -run 'TestContainerImageIdentitySupportWriter.*PostgresLive|TestContainerImageIdentityHeldSupportStorePostgresLive' \
  -count=1
```

It proves:

- one digest has a stable digest-derived identity while retaining three
  distinct supports;
- typed active-set support survives an incomplete-evidence hold and retires
  when the warning clears;
- a held pre-v3 legacy support is copied into the first typed set and the
  legacy row is deleted in the same fenced publication;
- a hold with no prior support creates no row;
- the reported legacy cleanup count matches the committed delete count;
- failed/non-active generations expose no rows, load no held support, return no
  activation snapshot, and cannot publish;
- activation ABA after a prior-support load rejects the stale publication and
  leaves the activation-cleared pointer unchanged;
- zero-output publication installs an explicit empty set and v3 writes no
  `fact_records` shadow row.
- reducer loaders preserve all 513 independently correlated supports across a
  500-row page boundary, while the public query still returns one digest;
- foreign global-union cursors before and after the support namespace include
  all or none of the support rows without decode errors;
- pre-pointer legacy and post-pointer typed reads preserve the same semantic
  digest, image, repository, outcome, strength, and source repository fields.

The in-process normalization/replay proof is:

```bash
go test ./internal/reducer \
  -run 'TestBuildContainerImageIdentitySupportSet|TestSupportWriter' \
  -count=1
```

It covers generation-independent set identity, semantic support-ID
recomputation, current/prior deduplication, exact held-reference loading, the
no-hold fast path, missing-prior behavior, and stale-claim rejection.

## Prove-the-theory-first performance evidence

All comparisons used the same Postgres 18 container, schema, synthetic
`registry.example.com` data, and 99,500-row support corpus.

### Publication

The current Go `fact_records` writer first measured 7,484.5 ms median and
7,686.1 ms p95 per 99,500-row publication. In the later fair eight-run
alternating SQL harness, the v2 shape measured 2,248.798 ms median,
2,412.581 ms p95, and 508,105,440 WAL bytes. The accepted anchor-free typed-set
shape measured 1,182.889 ms median, 1,419.171 ms p95, and 240,307,512 WAL
bytes. Against the matched harness that is 47.4% lower median latency, 41.2%
lower p95 latency, and 52.7% less WAL.

Concurrent 1,000-row publications also preserved or improved throughput:

| Shape | Prior median | Digest-v3 median | Change |
| --- | ---: | ---: | ---: |
| shared content | 130.175 ms | 123.672 ms | 5.0% faster |
| disjoint content | 235.042 ms | 117.899 ms | 49.8% faster |

The partial-overlap lanes at 25%, 50%, and 75% overlap completed without
failure. The terminal invariant was 16 committed sets, 16 active scope rows,
and zero orphan/mismatched supports.

### Bounded reads

The initial compatibility function was rejected after it expanded all 99,500
supports and exceeded 60 seconds. The accepted selector-first function uses a
keyset cursor and result limit. Against the same corpus:

| Selector | Rows | Execution | Shared hits |
| --- | ---: | ---: | ---: |
| exact digest | 1 identity | 1.938 ms | 26 |
| image reference | 1 identity | 1.427 ms | 27 |
| source repository, limit 10 | 10 identities | 1.348 ms | bounded GIN plus primary-key lookups |
| scope page | 500 identities | 159.962 ms | 4,680 |

Two consecutive 500-row scope pages were strictly ordered with no cursor
overlap.

Reducer consumers deliberately use a separate support-grain adapter rather
than the public digest aggregate. The first prototype was rejected: a forced
99,500-row materialization took 280.199 ms, wrote 11,931 temporary blocks, and
flattened too late to protect correlated fields. The accepted v3/legacy split
selects and keyset-pages support tuples before reconstructing envelopes:

| Support selector | Rows | Execution | Shared hits |
| --- | ---: | ---: | ---: |
| exact digest, adversarial 16-support shape | 16 supports | 0.249 ms | 24 |
| exact image reference | 1 support | 2.405 ms | 120 |
| source repository, limit 500 | 500 supports | 7.908 ms | 1,116 |
| broad scope page | 500 supports | 51.802 ms | 6,675 |

The broad page used an in-memory top-N heapsort (895 kB peak), wrote no
temporary blocks, and was 67.6% faster than the 159.962 ms public aggregate
scope page on the same corpus. Existing primary-key, image-reference,
repository, and source-repository GIN indexes were sufficient; no new index was
needed. A live 513-support regression proves page continuity, uniqueness, and
field correlation through all three reducer loaders.

### Warning-held prior support

`EXPLAIN (ANALYZE, BUFFERS, WAL)` on the accepted typed loader used
`container_image_identity_supports_image_ref_idx` under the exact active set:

| Held references | Returned | Execution | Shared hits |
| ---: | ---: | ---: | ---: |
| 1 | 1 | 0.133 ms | 14 |
| 1,000 | 1,000 | 1.294 ms | 448 |

The pre-v3 legacy-only path is transitional and bounded by exact scope,
generation, active status, and held references. On a synthetic 1,000-row
legacy scope it measured 0.680 ms for one held reference and 11.325 ms for all
1,000. The no-hold path performs no prior-support query.

The finished in-process normalization/hash benchmark ran 10 iterations three
times. Median samples were 6.823 ms for 1,000 current supports and 13.668 ms
for 1,000 current plus 1,000 held supports. The additional work is linear in
the explicitly held rows and does not affect the no-hold loader path.

## Promotion gates

The final clean B-7 run used the required comprehensive profile:

```bash
GOCACHE=$PWD/.gocache ESHU_POSTGRES_PORT=25432 \
  NEO4J_HTTP_PORT=27474 NEO4J_BOLT_PORT=27687 \
  bash scripts/verify-golden-corpus-gate.sh
```

It completed in 110 seconds with 518 passes, zero required failures, and one
advisory timing warning. Every drain reported `residual=0` and `dead_letter=0`.
The workload-scoped `CVE-2026-00010` comprehensive-profile assertion passed
with the expected runtime context and suppression state, proving the reducer
consumed support-grain identity evidence rather than the digest-level
presentation union.

Focused query/reducer/storage tests, their race variants, the live Postgres
support tests, telemetry verifier, and strict documentation build also passed
after the final implementation edits. Preliminary/final `eshu-code-review`
and `make pre-pr` remain the last local promotion steps before push.
