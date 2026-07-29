# Reducer-Derived Package Correlation Typed Decode Evidence (#4799)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Reducer-Derived Package Correlation Typed Decode (#4799)

No-Regression Evidence: package ownership, consumption, and publication
correlations move from hand-built residual maps to factschema typed structs,
canonical schemas, fixturepack payloads, and generated registry rows. The
runtime shape is still in-process reducer fact decode plus existing package
correlation writes; no queue domain, graph query, graph write, worker, or
runtime knob changes.

| Surface | Before | After |
| --- | --- | --- |
| Governed reducer-derived package correlation facts | Hand-built maps, no factschema schema coverage | Typed reducerderived v1 structs, canonical schemas, fixturepack valid/invalid payloads |
| Malformed persisted package correlation facts | Missing `package_id` could be skipped by consumers without a typed quarantine path | Missing `package_id` is rejected by the decode wrapper and counted as input-invalid while valid sibling facts continue |
| Payload-usage manifest coverage | 103 governed struct-backed fact kinds | 106 governed struct-backed fact kinds |
| Fact-kind registry coverage | 3 reducer_derived schema-version rows | 6 reducer_derived schema-version rows, adding ownership, consumption, and publication package correlations |
| Code coverage report | 4,190 files, 200,453 / 267,133 statements covered | 4,195 files, 200,707 / 267,639 statements covered |

Benchmark Evidence: `go test ./internal/reducer -run '^$' -bench
BenchmarkBuildSupplyChainImpactIndexWithQuarantine -benchmem -count=1` covered
the supply-chain index path with 2,000 advisories, each carrying one CVE, one
affected package, one OS package, and one reducer package-consumption
correlation. Result on this branch:
`BenchmarkBuildSupplyChainImpactIndexWithQuarantine-18 213 5379859 ns/op
6557235 B/op 100123 allocs/op`.

No-Observability-Change: malformed typed reads feed the existing
`eshu_dp_reducer_input_invalid_facts_total` counter, the structured
`reducer input_invalid fact quarantined` log, and
`Result.SubSignals["input_invalid_facts"]`. Normal reducer runs remain covered
by `eshu_dp_reducer_executions_total`, `eshu_dp_reducer_run_duration_seconds`,
and the durable `reducer_package_*` facts. The typed payload helpers and decode
wrappers emit no metric of their own because they do not add a new operational
stage.
