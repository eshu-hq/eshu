# #11031 finding first-seen stamp: performance and observability evidence

The criterion-6 publish-to-finding chain stamps each finding with its
earliest durable reducer write (`supply_chain_impact_finding_first_seen`,
migration 357), claimed with one `INSERT ... SELECT unnest ... ON CONFLICT
DO NOTHING` plus one `SELECT ... WHERE stable_fact_key = ANY` per pass,
inside the write transaction after fencing admission and under the
already-held conflict-domain lock. Each row's payload marshals pre-claim for
the pre-transaction publication set (byte-identical to the pre-#11031
build), then remarshals after the claim only when stamps resolved, with the
two stamp keys set on its provenance; digests are computed from the typed
struct before the stamp keys exist, so neither the content nor the identity
digest covers the stamp and a rewrite compares identical instead of
churning the changed set. The cassette admission cell
(`go/cmd/ifa/enqueue_advisory_groups.go`) claims the same way for replayed
rows, offering a pre-stamped payload's own instant so a replay keeps its
original first-seen.

No-Regression Evidence (#11031): the per-pass SQL is two set-based
statements no matter how many findings the pass carries. Stamping passes pay
a second marshal per row; unstamped passes skip the remarshal
(`len(stamps) > 0` guard) and keep the single pre-existing marshal.
Microbenchmark `BenchmarkFirstSeenApply11031` (200 production-shaped
1,656-byte payloads, arm64 dev host; stamped arm = pre-claim marshal plus
in-transaction remarshal, unstamped arm = pre-claim marshal alone):
stamped 5,419,504 ns/op vs unstamped 2,857,868 ns/op. The ~2,561,636 ns
delta over 200 rows (~12.8 us/row) is the stamp's whole marginal cost: one
extra marshal of the finding payload per row on stamping passes only.
Digest stability is proven, not asserted: the stamp-free unit-test path
writes byte-identical payloads, and `TestRoundsCutDiagnosticChangesNothing`
plus the full `internal/reducer/supplychain/core` suite pass with the stamp
wired in.

No-Observability-Change (#11031): this stage emits no metric of its own. A
claim error fails the pass through the existing writer error path, timed by
`eshu_dp_supply_chain_impact_write_tx_duration_seconds`; successful stamps
are observable in the `supply_chain_impact_finding_first_seen` table the QA
cohort reads directly. The per-file coverage row lives in
`docs/public/observability/telemetry-coverage-reducer-domains.md` beside the
supply-chain projection row.
