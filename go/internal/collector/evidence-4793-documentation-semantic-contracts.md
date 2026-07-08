# #4793 Documentation And Semantic Contract Evidence

Benchmark Evidence: on go1.26.4 darwin/arm64, the representative W1g
constructor benchmarks compare the legacy JSON marshal/unmarshal payload-map
shape against this branch's typed factschema bridge on the same inputs. The
documentation section fixture is one Confluence section with ordinal path,
persisted content, hashes, source refs, source metadata, and warnings; the
semantic documentation observation fixture is one redacted runbook observation
with source, chunk, provider, confidence, policy, redaction, freshness,
admission, and evidence refs. `go test ./internal/facts -run '^$' -bench
'Benchmark(DocumentationSection|SemanticDocumentationObservation)EncodeNoRegression'
-benchmem -count=5` measured documentation section legacy baseline
4618-4922 ns/op, 4846-4847 B/op, 87 allocs/op versus typed bridge 1176-1300
ns/op, 3376 B/op, 37 allocs/op; semantic observation legacy baseline
6958-7341 ns/op, 6356-6357 B/op, 128 allocs/op versus typed bridge 2323-2342
ns/op, 6032 B/op, 65 allocs/op. These constructors do no database, graph,
queue, or network work; terminal queue and row counts are unchanged at 0 for
the benchmarked path.

No-Regression Evidence: focused local proof passed
`go test ./internal/collector -run TestContractEncodeAdoptionRatchet -count=1`,
`go test ./internal/facts ./internal/doctruth ./internal/semanticdocs -count=1`,
`go test ./internal/collector/confluence ./internal/collector/documentationexport
./internal/collector/mediadoc ./internal/collector/ocrdoc -count=1`, and
`go test ./... -count=1` from `sdk/go/factschema`.
`scripts/verify-fact-kind-registry.sh && git diff --check` passed.
`make pre-pr` passed gofumpt, golangci-lint, build, vet, changed-package tests,
500-line cap, package-docs, replay/contract exactness gates, code coverage
report generation, perf-evidence, and the scoped race lane after this tracked
evidence note was added. The branch also adds direct legacy-shape equivalence
tests for documentation section and semantic documentation observation
encoding.

No-Observability-Change: #4793 changes only in-process payload-map construction
before existing collector envelopes or doctruth/semanticdocs fact emission. The
fact kinds, source refs, collector kind, confidence, generation, queue behavior,
reducer/projector boundaries, graph writes, Postgres rows, worker counts,
leases, retries, and telemetry instruments are unchanged. The existing
documentation, doctruth, semanticdocs, collector, reducer, and projector signals
remain the operator-facing path for diagnosis; no new metric, span, log key,
status row, queue stage, or dashboard contract is introduced.
