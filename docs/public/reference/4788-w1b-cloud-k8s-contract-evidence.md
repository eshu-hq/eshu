# W1b Cloud And Kubernetes Contract Evidence

Issue #4788 migrates Azure, GCP, and Kubernetes live collector emit paths from
inline payload maps and json-roundtrip factschema encoders to direct-map
factschema encoders. The change does not alter collector scheduling, claims,
leases, retries, graph writes, reducer queue behavior, or provider API calls.

Benchmark Evidence: `cd sdk/go/factschema && go test . -run '^$' -bench
BenchmarkW1bEncodeNoRegression -benchmem -count=5` on darwin/arm64
Apple M5 Max. Baseline was the json-roundtrip encoder path: Azure cloud
resource 9339-10427 ns/op, 7353 B/op, 139 allocs/op; GCP cloud resource
9403-9779 ns/op, 7415-7416 B/op, 160 allocs/op; Kubernetes live pod template
5073-5597 ns/op, 4518-4519 B/op, 108 allocs/op. After direct-map encoding on
the rebased branch: Azure cloud resource 524.7-721.0 ns/op, 1064 B/op,
12 allocs/op; GCP cloud resource 430.4-715.6 ns/op, 1088 B/op, 13 allocs/op;
Kubernetes live pod template 1010-1505 ns/op, 1560 B/op, 22 allocs/op. The
benchmark input shapes are the representative W1b typed payloads in
`BenchmarkW1bEncodeNoRegression`: one populated Azure cloud resource, one
populated GCP cloud resource, and one Kubernetes live pod template with a
container, image, labels, selector, and anchors.

No-Regression Evidence: `scripts/verify-golden-corpus-gate.sh --keep` with the
default NornicDB Compose backend passed after the final rebase. The gate landed
17 credentialed collector sources, drained to `fact_work_items_residual=0` and
`shared_projection_intents_nonterminal=0` on the first drain and both
maintenance drains, and completed with 412 required passes, 0 required failures,
1 advisory timing warning, and 105s wall time under the 1800s ceiling. Focused
post-rebase proof also passed: `go test ./internal/collector/azurecloud
./internal/collector/gcpcloud ./internal/collector/kuberneteslive
./internal/synth/gcp ./cmd/golden-corpus-gate -count=1`,
`ESHU_SYNTH_GCP_PARITY=1 go test ./internal/synth/gcp -run
TestParitySyntheticVsRecordedGCPShape -count=1 -v`,
`scripts/verify-fact-kind-registry.sh`,
`scripts/test-verify-golden-corpus-gate.sh`, and
`scripts/verify-cassette-author.sh`.

No-Observability-Change: the rewritten builders still flow through the existing
collector envelope and ingestion commit path, so operators keep the same
`ingestion commit stage completed` structured logs, per-collector `collector
commit succeeded` log with `fact_count`, scope/generation identifiers, the
existing runtime status endpoints, and the golden gate source-count/drain
assertions. No new worker, queue, lease, graph-write, retry, network, or API
surface was introduced; the only runtime change is replacing payload-map
construction with the generated typed factschema direct-map seam.
