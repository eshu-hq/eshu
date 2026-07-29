# #5469 tiered version-resolution evidence

## Accuracy contract

`version_resolution_tier` selects the strongest eligible deployment-truth
tier that makes a concrete version or artifact-identity claim. The resolver
uses the shared `truth.DeploymentTruthTier` vocabulary. It does not invent the
unshipped `declared_ref` tier.

The query path preserves these boundaries:

- a live cloud resource whose running digest matches the finding wins as
  `runtime_confirmed`;
- a strong CI/CD artifact match may win as `provenance_ci_declared`;
- a CI-declared digest that contradicts the finding remains visible as
  corroboration but cannot become the judged identity;
- `config_only` falls through from observed version to subject digest to image
  reference;
- agreement is `agrees`, `disagrees`, or `not_comparable`, so cross-axis
  version/digest comparisons are never reported as contradictions;
- list and explain run the same authorized cloud-runtime and read-time
  runtime-context probes before assembling these fields; the investigation
  packet omits both fields and skips those unused reads.

The reducer persists the matched CI deployment's own artifact digest and image
reference atomically. It never borrows the finding identity or combines fields
from different deployment records.

## TDD and focused proof

The performance repair started with
`TestBuildSupplyChainImpactFindingResultAllocationBudget`. It failed on source
commit `2ea49902be`'s parent with:

```text
allocations per result = 2, want <= 1
```

The finished code passes that budget while returning two corroboration
records. `TestNormalizedSupplyChainImpactMissingEvidenceLazyFastPath` and
`TestNormalizedSupplyChainImpactMissingEvidenceFallbacksPreserveContract`
also pin the allocation-free fast path and the trimmed, sorted, deduplicated
fallback behavior.

Commands run:

```bash
cd go
GOCACHE=$PWD/.gocache go test ./internal/query -count=1
GOCACHE=$PWD/.gocache go test ./internal/query ./internal/reducer ./internal/truth -count=1
GOCACHE=$PWD/.gocache go test -race ./internal/query -count=1

cd ../sdk/go/factschema
GOCACHE=$PWD/.gocache go test ./... -count=1

cd ../../../go
GOCACHE=$PWD/.gocache go test ./cmd/golden-corpus-gate/... -count=1
cd ..
bash scripts/test-verify-golden-corpus-gate.sh
```

All commands passed. The factschema source and fixture schema remain governed
by the module tests, including optional-field decode compatibility for older
findings.

## Prove-the-theory-first performance work

The first branch implementation copied the large finding row into several
helpers, allocated temporary candidate storage, over-sized the returned
corroboration slice, and copied missing-evidence values even when already
normalized.

A test-only pointer/lazy-allocation shim was benchmarked before production
changed. It used the existing tier order and compared its assembled result to
the current implementation with `reflect.DeepEqual` before timing.

Five two-second samples on Go 1.26.5, darwin/arm64, Apple M5 Max:

| Shape | ns/op range | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Pre-repair #5469 branch | 269.0-271.3 | 208 | 2 |
| Test-only measured shim | 115.0-115.8 | 128 | 1 |

The theory was accepted: pass rows by pointer, keep the four-candidate working
set in a fixed stack array, allocate the returned slice at its exact size, and
reuse already-normalized missing evidence. The test-only shim was then
deleted.

## Exact base versus finished performance

The final comparison uses exact base `ba2b7b80be85099690e8ee959fbb0a24cc012457`
and source commit `2ea49902be`. Both benchmarks use the same common row fields,
two-second duration, five samples, and a package-level result sink. The base
omits only the two fields that do not exist before #5469 and calls its
value-parameter assembler; the pointer parameter is part of the measured
optimization.

```bash
# exact base ba2b7b80be85
GOCACHE=$PWD/.gocache go test ./internal/query -run '^$' \
  -bench '^BenchmarkBuildSupplyChainImpactFindingResultBaseline$' \
  -benchmem -benchtime=2s -count=5

# finished #5469 source
GOCACHE=$PWD/.gocache go test ./internal/query -run '^$' \
  -bench '^BenchmarkBuildSupplyChainImpactFindingResult$' \
  -benchmem -benchtime=2s -count=5
```

| Metric | Exact base | Finished #5469 |
| --- | ---: | ---: |
| ns/op range | 143.3-144.5 | 95.64-96.84 |
| B/op | 16 | 128 |
| allocs/op | 1 | 1 |

CPU cost improves by about 30 percent and allocation count is maintained. The
remaining 112 B/op delta is the two corroboration records returned by the new
wire contract, not temporary classifier storage. Candidate work remains
strictly bounded to the four closed truth tiers.

## Cloud-runtime lookup theory and route proof

The first candidate repair added a property index to NornicDB's
`CloudResource.running_image_digest`. Against 50,000 synthetic cloud-resource
nodes, the index reported 50,000 entries but repeated zero-match probes still
took roughly 130-190 ms. That theory was rejected and no graph index ships.

The accepted design uses the reducer-owned `graph_node_owner.winning_row`
read model. The same query resolves the requested digest and checks the source
fact's active generation, tombstone state, and caller grants. A 100,000-row
synthetic owner ledger measured the same zero-match shape before and after the
partial `(running_image_digest, arn, uid)` expression index:

| Plan | Rows examined | Shared hits | Execution |
| --- | ---: | ---: | ---: |
| Unindexed sequential scan | 100,000 removed by filter | corpus scan | 7.55 ms |
| Indexed owner-ledger read | 1 index search, 0 matches | 3 | 0.038 ms |

The new plan is about 199 times faster for this worst-case zero-match lookup.
The behavior change is intentional rather than output-equivalent: the query
adds active-generation freshness and scoped authorization to the exact
digest-to-runtime-resource answer. The integration proof verifies the
expression index is selected, a scoped caller cannot observe a denied
resource, and a tombstoned source fact cannot supply runtime evidence:

```bash
ESHU_POSTGRES_TEST_DSN="$TEST_DSN" GOCACHE=$PWD/.gocache \
  go test -tags=integration ./internal/query \
  -run '^TestCloudResourceRuntimeDigestProductionVariantLive$' -count=1 -v
```

The actual route proof used the same isolated 50,000-node graph and
100,000-row owner ledger for exact base `ba2b7b80be85` and the finished code.
Each measurement is the median of 15 unique zero-match digests. NornicDB was
restarted before each cold comparison:

| Route | Exact base | Finished #5469 | Interpretation |
| --- | ---: | ---: | --- |
| Findings list | 173.516 ms | 0.503 ms | about 345 times faster; graph scan removed |
| Impact explain | 0.041 ms | 0.434 ms | corrected path now resolves runtime evidence; still sub-millisecond |
| Investigation packet | 0.051 ms | 0.020 ms | faster because its wire shape omits the enriched fields and their reads |

Warm repeated reads put both list implementations below one millisecond, so
the cold comparison isolates the graph-scan tail rather than claiming a
steady-state cache win. The temporary route harness was removed after the
measurement; production integration and handler tests retain the accuracy,
index-selection, and skipped-packet-read contracts.

## Live pipeline proof

The live B-7 gate ran after the performance source commit on the rebased
branch:

```bash
docker compose -f docker-compose.yaml down -v
GOCACHE=$PWD/.gocache ESHU_POSTGRES_PORT=25432 \
  NEO4J_HTTP_PORT=27474 NEO4J_BOLT_PORT=27687 \
  bash scripts/verify-golden-corpus-gate.sh
```

Result:

```text
summary: 512 pass, 0 required-fail, 1 advisory-warn
PASS: B-7 golden corpus gate green (elapsed 104s)
```

The advisory was the graph-query wall-clock check at 24 seconds. The required
pipeline wall time, bootstrap, collection, first drain, and maintenance drains
all passed. Every suppression and reducer drain reported zero residual and
zero dead-letter work. The live query assertions included the comprehensive
OS-package profile and the #5466 suppression fields merged immediately before
this rebase.
