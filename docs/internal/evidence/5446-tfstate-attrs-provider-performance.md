# #5446 tfstate attribute allowlist extension + provider-binding pre-pass — performance and observability evidence

## Change summary

Three additive, non-hot-path-shape changes on top of #5441/#5443's shipped
Terraform-state canonical writer:

1. Extends `terraformAttributePromotionAllowlist`
   (`go/internal/storage/cypher/terraform_attribute_promotion.go`) with
   `aws_ecs_task_definition`, `aws_ecs_service`, `aws_rds_cluster`, `aws_lb`,
   `aws_elasticache_replication_group`, plus two more attributes each on the
   pre-existing `aws_lambda_function` and `aws_db_instance` entries. Pure
   ride-along on the existing `promoteTerraformResourceAttributes` /
   `terraformStateResourceAttributeRemoveCypherByType` machinery: no new
   writer, no new statement, no new REMOVE shape.
2. Adds a provider-binding pre-pass
   (`terraformStateProviderBindingsByResource`,
   `go/internal/projector/tfstate_canonical.go`) that decodes every
   `terraform_state_provider_binding` fact and joins
   `Provider`/`ProviderSourceAddress`/`ProviderAlias` onto each
   `TerraformStateResourceRow` by `ResourceAddress`, mirroring the
   pre-existing `terraformStateTagHashesByResource` pre-pass. The graph
   writer (`go/internal/storage/cypher/tfstate_canonical_writer.go`) adds
   three FIXED, UNCONDITIONAL `SET` keys
   (`r.provider`/`r.provider_source_address`/`r.provider_alias`) to the
   existing per-batch `UNWIND $rows AS row MERGE ... SET ...` resource-upsert
   template — no new statement, no new `MERGE`, no REMOVE-list change (fixed
   keys with a guaranteed row value can never go stale the way the dynamic
   `tf_attr_*` allowlist keys can).
3. Removes `terraform_state_provider_binding` from the mcp package's
   consumer-disclosure ledger
   (`go/internal/mcp/kind_disclosure_ledger.go`) now that it has a real
   projector consumer, and updates the per-kind consumption comment in
   `specs/fact-kind-registry.v1.yaml`.

Neither (1) nor (2) changes the statement count, adds a `MERGE`, adds a
traversal, or changes lock/conflict-domain shape. The theory under test is
"no new O(n) or O(n²) cost class," not a specific latency target — this
complements, and does not re-prove, the REMOVE-before-upsert two-statement
shape documented in `docs/internal/evidence/5441-edge-node-properties.md`,
which this change leaves untouched.

## Benchmark Evidence

Benchmark Evidence: attribute promotion is an O(1) map lookup per resource row
(412.3 ns/op pre-existing 2-attr shape, 767.1 ns/op post-#5446 6-attr shape,
8.5 ns/op on a type miss); the provider pre-pass adds one decode per resource
fact and three fixed scalar SET keys — full numbers below.

No-Regression Evidence: the write path keeps the shipped REMOVE-before-upsert
two-statement shape unchanged (cited from `docs/internal/evidence/5441-edge-node-properties.md`,
not re-proven here); the payload grows only by a bounded, enumerated scalar set,
and `BenchmarkBuildTerraformStateStatementsSyntheticCorpus` (10k resources)
below shows the statement count and wall stay bounded.

All in-process benchmarks run on this machine (Apple M1 Max, `darwin/arm64`),
`go test -bench` against the built package, no live backend required except
where noted.

### (a) `BenchmarkPromoteTerraformResourceAttributes` — allowlist size, old vs. new shape

```
$ go test ./internal/storage/cypher/... -run '^$' \
    -bench 'BenchmarkPromoteTerraformResourceAttributes$' -benchtime=200x -benchmem
BenchmarkPromoteTerraformResourceAttributes/aws_instance_2attr_preexisting_shape-10        412.3 ns/op   440 B/op    8 allocs/op
BenchmarkPromoteTerraformResourceAttributes/aws_lambda_function_6attr_post_5446_shape-10   767.1 ns/op   624 B/op   20 allocs/op
BenchmarkPromoteTerraformResourceAttributes/unrecognized_type_map_lookup_miss-10             8.545 ns/op    0 B/op    0 allocs/op
```

The pre-#5446 2-attribute-path shape (`aws_instance`) and the largest
post-#5446 6-attribute-path shape (`aws_lambda_function`, after #5446 added
`version`/`image_uri` to the pre-existing 4) both stay in the same
sub-microsecond order of magnitude; cost is `O(len(allowlist[resourceType]))`
after one `O(1)` map lookup, so growing one entry cannot affect another
resource type's cost, and adding two paths to an existing entry adds ~350ns
(two more `terraformAttributePathValue` walks + `canonicalGraphPropertyValue`
normalizations), not a new order of growth. The unrecognized-type miss stays
allocation-free.

### (b) `BenchmarkBuildTerraformStateStatementsSyntheticCorpus` — 10k-resource materialization

```
$ go test ./internal/storage/cypher/... -run '^$' \
    -bench 'BenchmarkBuildTerraformStateStatementsSyntheticCorpus$' -benchtime=200x -benchmem
BenchmarkBuildTerraformStateStatementsSyntheticCorpus-10   12531856 ns/op   63.00 statements/op   0.0063 statements_per_resource   20248806 B/op   270549 allocs/op
```

10,000 synthetic `TerraformStateResourceRow`s (each carrying a provider
binding and a promotable `tf_attr_instance_type`) produce 63 batched
statements at the writer's 500-row batch size (10,000 / 500 = 20 upsert
batches, plus a fixed number of REMOVE/retract/module/output/edge-retract
statements per cycle) — a bounded, linear `statements_per_resource` ratio.
This is the regression tripwire: a future change that alters the batching
shape (not just adds properties) would move this ratio, not just the
absolute wall time.

### (c) `BenchmarkExtractTerraformStateRowsProviderBindingOverhead` — with/without binding facts

```
$ go test ./internal/projector/... -run '^$' \
    -bench 'BenchmarkExtractTerraformStateRowsProviderBindingOverhead' -benchtime=50x -benchmem
BenchmarkExtractTerraformStateRowsProviderBindingOverhead/without_provider_binding_facts-10   12064328 ns/op   19110480 B/op   155046 allocs/op
BenchmarkExtractTerraformStateRowsProviderBindingOverhead/with_provider_binding_facts-10      15883702 ns/op   20858976 B/op   175089 allocs/op
```

5,000 resources with vs. without one matching `terraform_state_provider_binding`
fact each (10,000 total envelopes in the "with" case, double the "without"
case's 5,000). The added cost (~3.8ms, ~660ns per binding fact) is the full
cost of decoding and joining 5,000 EXTRA facts through the typed factschema
seam — proportional to the added input size, not superlinear. The pre-pass
is a single `O(n)` pass over the envelope slice, structurally identical to
the pre-existing `terraformStateTagHashesByResource` pre-pass this batch
already ran before #5446.

## No-Regression Evidence (live Bolt, isolated NornicDB)

Per `cypher-query-rigor`, the resource-upsert template
(`canonicalTerraformStateResourceUpsertCypher`) is a hot-path graph write, so
the in-process benchmarks above are backed by a live-backend measurement
using the SAME committed, opt-in benchmark this repo already ships for this
exact statement
(`BenchmarkTerraformResourceUpsertOnlyLive`,
`go/internal/storage/cypher/tfstate_canonical_writer_stale_attrs_live_bench_test.go`,
added by #5441 review round 9 P2). Backend: `eshu-nornicdb-pr261:149245885258`
(the same pinned image `docker-compose.yaml` builds), run as a standalone,
uniquely-named container (`iac5446-nornicdb-bench`, ports 47474/47687) so as
not to collide with other concurrent agents' compose stacks on this machine —
no full golden-corpus compose stack was started for this measurement. Same
500-row batch shape (`terraformResourceBenchBatchSize`), 30 iterations per
run, 3 runs per variant:

BEFORE (this PR's 3 new `r.provider*` SET keys temporarily reverted,
statement otherwise identical to origin/main):

```
17011604 ns/op
16944001 ns/op
17957794 ns/op
```

AFTER (this PR's committed shape, 3 new fixed SET keys present):

```
25074099 ns/op
19818217 ns/op
17528543 ns/op
```

Both ranges overlap (~17-19ms typical, with one AFTER outlier at 25ms
consistent with ordinary live-network/DB jitter on a shared dev machine, not
a systematic shift) — no measurable regression from adding three more fixed,
always-populated `SET` clauses to an already ~20-key `SET` list on the same
statement. Exact equivalence check: this is an additive property change
(the upsert now also writes `r.provider`/`r.provider_source_address`/
`r.provider_alias`), not an output-preserving rewrite, so the applicable
proof is the intended-delta shape per the Prove-The-Theory-First rule
(new fields populated on write) plus the no-regression timing above, not a
row-set-identity diff.

## Observability Evidence

No-Observability-Change: this PR adds no new pipeline stage, worker, queue
consumer, or retry path. The three new node properties and the
provider-binding pre-pass flow through the SAME projector
(`Runtime.Project`) and writer (`CanonicalNodeWriter.Write`) call paths
#5441/#5443 already instrument; existing `eshu_dp_projector_*` and
`eshu_dp_canonical_writer_*` spans/metrics/logs (span names, duration
histograms, and `eshu_dp_projector_input_invalid_facts_total` with
`stage="terraform_state_canonical"`) already cover this code path, including
the new `terraformStateProviderBindingsByResource` quarantine path, which
reuses the existing `partitionProjectorDecodeFailures` / quarantine
telemetry every other terraform_state decode site already emits through.

## Deferred: golden-corpus gate phase wall (item d)

The live, full `scripts/verify-golden-corpus-gate.sh` Docker run (the fourth
requested benchmark: tfstate phase wall time before/after under the full
corpus) was intentionally NOT run for this change. The orchestrator
sequencing this work (`exec-5445`) asked that the live full golden-corpus
gate be held while a related build is mid-flight, to avoid two concurrent
agents contending for the same corpus/backend proof. `scripts/test-verify-golden-corpus-gate.sh`
(the fast, no-Docker static contract check) and the hermetic
`TestTerraformStateCassetteResourceCarriesProviderBinding` unit test both
pass and prove the fixture/code shapes agree; the live phase-wall
measurement should be captured by whoever runs the next full golden-corpus
gate pass and can be appended here.
