# W1e Observability, OCI, Package, And Terraform State Contract Evidence

Issue #4791 migrates the W1e live collector emit paths for observability,
OCI registry, package registry, and Terraform state from helper or inline
payload maps to canonical `factschema.Encode*` payload construction. The
change does not alter collector scheduling, claims, leases, retries, graph
writes, reducer queue behavior, provider API calls, or persisted fact kinds.

Benchmark Evidence: on go1.26.4 darwin/arm64, representative W1e constructor
benchmarks compared `origin/main` baseline emitters against this branch's
typed factschema emitters on the same inputs. Grafana observed rule baseline:
4484/4767/4600 ns/op; branch: 4814/4474/4340 ns/op. OCI manifest baseline:
4289/4294/4516 ns/op; branch after the direct descriptor map:
4739/4549/4142/4081/4041 ns/op. Package registry version baseline:
3517/3404/3356 ns/op; branch: 3453/3278/3237 ns/op. The input shapes are one
representative observed alert rule, one OCI manifest with config plus one
layer, and one Maven package version with artifact URLs and checksums. These
constructors perform no database, graph, queue, or network work, so terminal
queue and row counts are unchanged at 0 for the benchmarked path.

No-Regression Evidence: `make pre-pr` passed gofumpt, golangci-lint, build,
vet, changed-package tests, 500-line cap, package docs, exactness/telemetry
gates, and the scoped race lane for the changed collector packages. Focused
reruns passed `GOCACHE=/tmp/eshu-codex-gocache-4791 go test
./internal/collector ./internal/collector/grafana ./internal/collector/loki
./internal/collector/tempo ./internal/collector/prometheusmimir
./internal/collector/ociregistry ./internal/collector/packageregistry
./internal/collector/terraformstate -count=1` from `go/`, and
`GOCACHE=/tmp/eshu-codex-gocache-4791 go test ./... -count=1` from
`sdk/go/factschema`. The branch also keeps
`TestContractEncodeAdoptionRatchet` green so adopted W1e emitters keep calling
the canonical factschema encoders.

No-Observability-Change: W1e #4791 changes only in-process payload
construction before the existing fact envelope commit path. Collector request,
fact-volume, duration, redaction, warning, freshness, and scan-status signals
remain covered by the existing family rows in
`docs/public/observability/telemetry-coverage.md` for Grafana,
Prometheus/Mimir, Loki, Tempo, OCI registry, package registry, and Terraform
state. The new helper files are documented with `No-Observability-Change:`
rows in that telemetry coverage contract, and no new metric, span, log key,
queue stage, row table, worker, lease, or retry boundary is introduced.
