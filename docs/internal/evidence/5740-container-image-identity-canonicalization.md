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
  -run 'TestContainerImageIdentitySupportWriter.*PostgresLive|TestContainerImageIdentityHeldSupportStorePostgresLive|TestContainerImageIdentityCurrentSupportFacts(PreserveGrain|LegacyCutoverParity)PostgresLive|TestContainerImageIdentityV3MigrationPopulatedUpgradeLive' \
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
- malformed, non-UTF8, truncated, short-ID, and foreign-namespace cursors use
  lexical namespace ordering without decode errors or broken keyset bounds;
- populated first upgrade takes a real lock barrier, leaves no partial support
  store on lock timeout, creates no eager typed rows, preserves legacy read
  authority, and keeps trigger/catalog/activation state stable on restart;
- the first fenced digest-v3 publication after upgrade carries a warning-held
  legacy support and atomically removes the exact 10,000-row legacy generation;
- `BUILT_FROM` and `DERIVED_FROM` use the writer-accepted effective support set,
  retain held edges, make no graph calls after a rejected write, and match the
  compatibility decision builders row for row.
- warning-held base-image supports retain their repository attribution during
  the first typed-set publication, so the accepted support set still emits the
  valid child-to-base `DERIVED_FROM` edge;
- readiness by mutable image reference includes all 513 current digests rather
  than truncating at the first 500 support envelopes;
- support keyset pagination remains complete for prefix-related scope IDs under
  both the default and ICU database collations; reducer callers page the raw
  `(scope_id, digest, support_id)` tuple owned by the support function instead
  of reordering its encoded FactID;
- the older image-ref-v2 cutover marker and the digest-v3 claim latch advance
  `claimed` to `running` atomically, while a synthetic legacy-v2 attempt keeps
  `container_image_identity_v3_required = FALSE` and an empty v3 authorization.

The in-process normalization/replay proof is:

```bash
go test ./internal/reducer \
  -run 'TestBuildContainerImageIdentitySupportSet|TestSupportWriter' \
  -count=1
```

It covers generation-independent set identity, semantic support-ID
recomputation, current/prior deduplication, exact held-reference loading, the
no-hold fast path, missing-prior behavior, and stale-claim rejection.

The writer-accepted graph regressions run explicitly with:

```bash
go test ./internal/reducer \
  -run 'TestContainerImage(BuiltFromSupportRowsMatchDecisionRows|DerivedFromSupportRowsMatchDecisionRows|IdentityHandlerProjectsWarningHeldEffectiveSupports|IdentityHandlerRejectsMissingEffectiveGraphProjection|IdentityHandlerDoesNotProjectRejectedPublication)' \
  -count=1
```

## Prove-the-theory-first performance evidence

Performance Evidence: OLD and NEW measurements below use synthetic
`registry.example.com` inputs on the same Apple M5 Max host. Postgres plans use
the same Postgres 18 container, warm storage, selectors, row counts, and
buffers. The graph benchmarks compare equivalent normalized decision and
support inputs. The migration comparison intentionally changes behavior: OLD
eagerly converted all active legacy facts into an unused typed set during
startup; NEW retains legacy read authority until the first fenced publication.

No-Observability-Change: existing reducer execution/run duration,
`eshu_dp_container_image_identity_decisions_total`,
`eshu_dp_container_image_identity_retirements_total`, Postgres query-duration,
queue residual/dead-letter status, and structured reducer failure reporting
cover the changed paths. Missing effective projection is a new fail-closed
validation error surfaced through those existing reducer error and queue
signals; it adds no new retry or operational failure class, runtime label, or
worker. The final live gate must end at zero residual and dead-letter work
items.

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

### Populated upgrade

The rejected OLD `092a` eager backfill and accepted NEW upgrade were run in
isolated schemas containing the same 10,000 synthetic active legacy facts and
10,000 reducer work items. The OLD SQL was loaded from pre-repair commit
`7eb43b2c7304474e6b3335a46df97c14e3044272`; the throwaway timing harness was
removed after the proof. OLD took 365.293 ms and created one typed set plus
10,000 support rows. NEW full upgrade took 56.600 ms and created no typed rows,
an 84.5% startup improvement despite timing more migration work. Its idempotent
restart took 18.690 ms. A five-run NEW confirmation had 53.987 ms first-upgrade
and 18.283 ms restart medians.

NEW preserves all 10,000 legacy rows through the compatibility view. The first
fenced v3 publication is the conversion seam: the live test proves it carries
the requested held support, moves the pointer, and deletes all 10,000 legacy
rows atomically. Explicit `pg_locks` barriers prove bounded lock timeout and
correct handling of scope activation immediately before and after trigger
installation.

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

The repaired cursor parser was also measured by swapping OLD and NEW function
definitions against the same warm 99,500-row corpus and running five broad
500-row pages each with `EXPLAIN (ANALYZE, BUFFERS, WAL, FORMAT JSON)`. OLD had
a 58.282 ms median; NEW had a 56.755 ms median, 2.6% faster. Both used 6,744
shared-buffer hits and wrote zero temporary blocks. The safe parser therefore
fixes malformed-cursor accuracy without regressing the production broad-page
path.

The final caller review found a distinct pagination boundary: a canonical
support FactID hex-encodes the raw tuple, so ordering the encoded identifier is
not equivalent to ordering prefix-related raw values (`a` and `aa`). A live
501-plus-1 boundary first returned 501 of 502 rows through the production
callers. The accepted loader leaves ordering and pagination inside the support
function, validates that every decoded tuple advances, and rejects malformed,
duplicate, or cross-stream FactIDs. The supply-chain loader retains one
statement snapshot and one round trip, but gives the legacy evidence stream and
canonical identity stream independent cursors, limits, ordinals, and completion
state. Its suppression sentinel can stop only the legacy tail while identity
pagination continues.

Several fixes were measured before production code was selected. Re-encoding
the raw tuple into a sortable framed FactID inside the function was rejected:
the 500-row source-repository selector slowed 19.5% and the two broad pages
slowed 35.9% and 66.1%. Decoding and sorting an outer 500-row page was also
rejected after the source-repository path slowed 114.5%. Issuing separate
supply-chain queries was correct but regressed its matched median/p95 from
68.737/75.407 ms to 74.774/97.156 ms.

The accepted direct callers and one-statement dual pager were then measured in
20-run alternating Go store calls against the same warm Postgres 18 corpus:

| Production call | OLD median / p95 | NEW median / p95 |
| --- | ---: | ---: |
| CI/CD, digest with 16 supports | 18.746 / 24.628 ms | 5.856 / 8.080 ms |
| SBOM, digest with 16 supports | 18.294 / 24.173 ms | 5.372 / 7.109 ms |
| supply chain, repository with 500 rows | 55.812 / 62.442 ms | 35.721 / 37.097 ms |

All paired calls returned identical FactID sets. The default-collation and ICU
live regressions each returned all 502 prefix-boundary rows through production
callers, including the long-prefix row after the 500-row boundary.

### Writer-accepted graph rows

The accepted effective-support projection avoids converting the support set
back into public decision objects. The longer matched benchmark ran five 500
ms samples per shape:

```bash
go test ./internal/reducer -run '^$' \
  -bench '^BenchmarkContainerImageIdentityGraphRows$' \
  -benchmem -benchtime=500ms -count=5
```

| Builder | Rows | OLD decisions median | NEW supports median | Change |
| --- | ---: | ---: | ---: | ---: |
| `BUILT_FROM` | 1,000 | 206.589 us | 167.445 us | 18.9% faster |
| `BUILT_FROM` | 5,000 | 848.540 us | 735.437 us | 13.3% faster |
| `DERIVED_FROM` | 1,000 | 196.923 us | 200.949 us | 2.0% slower |
| `DERIVED_FROM` | 5,000 | 863.516 us | 819.291 us | 5.1% faster |

At 5,000 rows, `BUILT_FROM` allocations fell from 25,013 to 20,014 and bytes
from 2,354,113 to 2,274,129. `DERIVED_FROM` retained the same 25,013 allocations
and approximately 2,818,882 bytes. The representative 5,000-row combined graph
path improves; the 1,000-row `DERIVED_FROM` result is near-neutral and is
reported rather than generalized away. Direct differential tests prove both
builders return identical rows for equivalent inputs.

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

### Review-remediation measurements

The readiness and locale-ordering reviews were proved before implementation
against the same isolated Postgres 18 service. With 700 digests sharing one
synthetic mutable reference, the old readiness path returned only 500 rows and
took 45.016 ms with 21,296 shared hits. The grouped current-support candidate
returned all 700 rows in 1.766 ms with 5,142 shared hits; neither plan read or
wrote temporary blocks. The finished 513-digest live regression returns 513
rows for both `container_image.identity` and `scanner_worker.analysis`.

An ICU `en-US` database ordered mixed-case identifiers differently from UTF-8
byte order and reproduced duplicate support rows across a two-row cursor page.
The bytewise tuple candidate took 1.820 ms versus 1.889 ms for the old raw-text
support page on the same warm corpus, with 5,142 shared hits and no reads or
temporary blocks in either plan. The finished ICU regression pages four mixed-
case scopes exactly once, and static contracts cover all three reducer callers.

The v2-marker/v3-latch compatibility repair adds no query or lock. Its exact
100,000-row marker-lock harness measured 100 one-row marker inserts at
203.709 us median and 335.042 us p95, below the existing 1,301.150 us p95
contribution budget. The populated 10,000-row migration and immediate rerun
completed in 34.742 ms and 9.551 ms respectively after the final function
replacement; the lifecycle test also proved lock-timeout rollback and stable
catalog state across repeated bootstrap applications.

The PR's first full GitHub check set exposed a minimal-schema drift in the
real-Postgres contention harness: production Claim/ClaimBatch SQL referenced
the two digest-v3 latch columns, but the deliberately reduced test schema still
declared only the image-ref-v2 fields. A five-column static contract went red
for the missing fields before the harness was corrected. GitHub's exact
race-enabled PostgreSQL 18 command then passed every contention, fencing,
fairness, and sign-in-policy test. Its 5,000-row rank-once candidate select
measured 26.836 ms against the existing 8-second latency budget.

## Promotion gates

The final clean B-7 run used the required comprehensive profile:

```bash
GOCACHE=$PWD/.gocache ESHU_POSTGRES_PORT=25432 \
  NEO4J_HTTP_PORT=27474 NEO4J_BOLT_PORT=27687 \
  bash scripts/verify-golden-corpus-gate.sh
```

It completed in 112 seconds with 517 passes, zero required failures, and two
advisory timing warnings. Every drain reported `residual=0` and `dead_letter=0`.
The workload-scoped `CVE-2026-00010` comprehensive-profile assertion passed
with the expected runtime context and suppression state, proving the reducer
consumed support-grain identity evidence rather than the digest-level
presentation union.

Focused query/reducer/storage tests, their race variants, the live Postgres
support tests, telemetry verifier, and strict documentation build also passed
after the final implementation edits. Preliminary/final `eshu-code-review`
and `make pre-pr` remain the last local promotion steps before push.
