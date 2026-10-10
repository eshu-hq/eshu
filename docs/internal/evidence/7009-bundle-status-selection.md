# #7009 live bundle status selection evidence

The live evidence bundle needs queue, scope, generation, stage, domain, collector, and semantic status, plus the graph repository count. It does not render Terraform-state last serials or recent warnings. Its status read now starts from `FullSnapshotSelection()` and sets only `SkipTerraformStateEvidence=true`. Collector fact evidence and registry collector reads remain enabled. Full and index status keep their Terraform reads.

## Theory and regression

The independently reviewed mapper-level theory fixture in `go/internal/query/evidence_bundle_selection_theory_test.go` and `testdata/evidence_bundle_selection_expected.json` compared complete stamped bundles at fixed timestamps with and without populated Terraform serial/warning rows. The frozen expected bundle includes health, queue, scope, generation, stage, bounded domain backlog, collector readiness, semantic profile, identity, redaction, validation, and bundle ID derived from the frozen expected body. Healthy, degraded, stalled, and empty cases passed. Mutations of collector fact evidence, health/reasons, stage/scope, domain rows/truncation, semantic profile, and queue changed the expected output. The qualified theory receipt is private at `7009-bundle-selection-theory-20261003/corrected-receipt.json` in the local latency harness; that receipt is not deployment proof.

The route regression was written before the handler change. Against the original full selection, `go test ./internal/query -run '^TestEvidenceBundleSelectsAllExceptTerraformState$' -count=1 -v` failed for the intended reason: HTTP 500, `load status: read status snapshot: terraform evidence unavailable`, rather than the expected 200. Direct exit was 1; wall time was 6.01 seconds. After the one-flag change, the selected query tests passed with direct exit 0 and wall time 7.52 seconds. They cover the new route, the frozen theory, existing live bundle behavior, full/index selection and Terraform error propagation, retained-reader errors, cancelled context, and bundle validation failure. A focused follow-up added an already-expired deadline and complete typed bundle parity between Terraform-only-error success and an otherwise-identical successful baseline, normalizing only CreatedAt and BundleID. That selected test passed with direct exit 0 in 6.95 seconds. Existing nil-reader and failed graph-count ambiguity tests were included.

The selected actual Postgres selection and telemetry tests passed with direct exit 0 and wall time 57.01 seconds: `TestReadStatusSnapshotFilteredSkipsOnlyTerraformEvidence`, `TestReadStatusSnapshotFilteredFullSelectionIssuesHeavyFactQueries`, and `TestSkippedTerraformStatusReadEmitsNoTerraformPhase`. These assert that the existing storage flag skips only the two Terraform evidence reads, preserves other reads, and emits no Terraform read phase when skipped. No SQL changed.

A Terraform-only read failure no longer fails the bundle request because that read is not issued. A retained status-read error still fails the request; there is no fallback after such an error. Validation, auth policy, graph-count ambiguous-zero handling, health classification, domain truncation, and bundle redaction stay on their existing paths. No-Observability-Change: The selection adds no metric or log key; existing status read-phase telemetry distinguishes skipped Terraform work from retained reads.

## Limits

A previously observed standalone Terraform status-read duration of 418 ms came from a read-only transaction on the Postgres writer (`pg_is_in_recovery=false`); it is neither a replica measurement nor an HTTP bundle saving. The standalone writer observation is separate from the native reader comparison below. Deployed p95 remains unmeasured. Deployed after-change behavior and performance remain **NOT_CHECKED**. The local fixture does not prove production workload cost or DB saturation behavior.

## Performance Evidence: native reader comparison

A private overlay test mounted the real evidence handler and real PostgreSQL `StatusStore` over one read-only, repeatable-read transaction on the QA replica. Before any status reads, the exact session reported `pg_is_in_recovery=true`, `transaction_read_only=on`, and repeatable-read isolation. The identity query also supplied the one transaction timestamp used for every status read. The test enforced a two-second statement timeout, five-second request deadlines, a 45-second transaction deadline, and rollback. The controller ran the selected test once and reaped its owned port forward.

Eight sequential requests used order full, selected, selected, full, full, selected, selected, full. Each full read issued 26 SQL statements; each selected read issued 24. The test asserted that only the last-serial and recent-warning statements disappeared. Every retained SQL shape and argument matched in order. All eight complete, validated bundle bodies matched after normalizing only the wall-clock CreatedAt field and its content-derived BundleID.

| Request | Selection | Native route time (ms) | SQL statements |
| --- | --- | ---: | ---: |
| 1 | Full | 2867.227 | 26 |
| 2 | Selected | 1526.795 | 24 |
| 3 | Selected | 1492.240 | 24 |
| 4 | Full | 1951.279 | 26 |
| 5 | Full | 1949.244 | 26 |
| 6 | Selected | 1514.358 | 24 |
| 7 | Selected | 1503.868 | 24 |
| 8 | Full | 1900.473 | 26 |

Median full time was 1950.2615 ms (1.950 seconds); selected time was 1509.113 ms (1.509 seconds), a 22.62 percent reduction in this local comparison. Direct exit was 0; the eight-request run took 15.13 seconds. This is local native handler and storage evidence using representative replica data, not a deployed endpoint distribution or physical cold-cache comparison. The graph repository count was fixed at 809; graph latency, HTTP transport, auth, and production reader freshness/fencing were excluded. No sub-second, p95, capacity, or whole-platform health claim follows from four samples per selection.
