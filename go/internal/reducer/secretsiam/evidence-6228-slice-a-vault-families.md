# Evidence: slice-A vault-family coverage adds no hot-path cost (#6228)

Records the performance and observability evidence for the slice-A offline
coverage commit (authenticates_vault_role, uses_vault_policy), so the
hot-path evidence gate (`scripts/verify-performance-evidence.sh`) has a
tracked, in-repo record specific to this change rather than relying on PR
prose.

## Change shape

Additive only: one offline chain-projection helper plus its unit test in
`go/internal/reducer/secretsiam/secrets_iam_chain_projection*.go`, new Ifá
Odu and materialized-edge coverage files for the two vault families, two
registry rows in `single_type_families.go`, one `#nosec G101` comment, and
lowercase-to-exported identifier renames in
`go/internal/storage/cypher/cloud/aws/iam/secrets_graph_writer.go` whose
Cypher template text is byte-identical (rename-only diff, no query text
changed). No batch size, worker knob, lease, retry, queue path, or telemetry
registration is modified (`go/internal/telemetry/` is untouched by this
branch).

## Benchmark Evidence:

- **Baseline / after** (same host, AMD EPYC 9R14, go1.26.2 linux/amd64,
  `-benchtime 100x -count=1`, `go/internal/reducer/secretsiam`, hermetic Go
  test, no graph backend):
  - `BenchmarkBuildSecretsIAMTrustChainReadModels`: base d76b2aeea2
    47,501,161 ns/op, 34,703,085 B/op, 332,332 allocs/op → branch
    46,763,261 ns/op, 34,703,673 B/op, 332,334 allocs/op.
- Deltas are noise (-1.6% time, +0.002% bytes, +2 allocs). The benchmark
  drives `BuildSecretsIAMTrustChainReadModels`, the first stage of the new
  `ProjectSecretsIAMGraphRows` helper, so the touched path is measured
  directly. (After-measurement ran on the branch worktree with only
  non-runtime comment/doc/script edits uncommitted.)

## No-Regression Evidence:

No existing hot path changed shape: the writer diff renames identifiers
only, the registry diff adds rows, and the new projection helper runs
offline inside the Ifá guards, never in the reducer worker pool, so no
existing budget holder can regress. Backend/version: no Cypher text of an
existing writer altered, no batch size or worker knob changed. Input shape:
benchmark corpus of 2,000 service accounts (16,000 fact envelopes → 2,000
exact trust chains); chain-projection unit test of 8 envelopes → 3 edge
rows (1 authenticates, 1 uses-policy, 1 grants-read) + 1 secret-path node;
Ifá cassette families pin 2 expected edges (authenticates) and 3 expected
edges (uses-vault-policy). No queue is involved (offline only), so there
are no terminal queue counts to report. Live proof (projection of the new
rows at corpus scale) belongs to the CI live matrices at landing.

## No-Observability-Change:

No metric, span, log key, or status surface added or altered (verified: no
otel/prometheus/telemetry/log/span additions in the diff, and
`go/internal/telemetry/` is untouched). The new helper emits nothing of its
own: the chain stays covered by `eshu_dp_secrets_iam_reducer_trust_chains_total`
and `eshu_dp_secrets_iam_posture_observations_total`, the graph rows by
`eshu_dp_secrets_iam_graph_edges_written_total` and
`eshu_dp_secrets_iam_graph_skipped_total`, and malformed facts quarantine
per-fact through `eshu_dp_reducer_input_invalid_facts_total` (folded into the
projection (secrets/IAM graph) row in
`docs/public/observability/telemetry-coverage.md`, line-neutral: the doc is
grandfathered at its cap). Nothing for an operator
to watch beyond existing secrets/IAM telemetry.
