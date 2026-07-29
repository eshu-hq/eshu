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
- list, explain, and investigation-packet routes run the same authorized
  cloud-runtime and read-time runtime-context probes before assembling these
  fields.

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
PASS: B-7 golden corpus gate green (elapsed 107s)
```

The advisory was the graph-query wall-clock check at 22 seconds. The required
pipeline wall time, bootstrap, collection, first drain, and maintenance drains
all passed. Every suppression and reducer drain reported zero residual and
zero dead-letter work. The live query assertions included the comprehensive
OS-package profile and the #5466 suppression fields merged immediately before
this rebase.
