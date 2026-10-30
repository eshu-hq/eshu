# Evidence: scanner manifest digest pins carry no performance or observability change (#8878)

The digest-pin change (`manifest.go`, `manifest_test.go`) swaps one
never-published digest constant for a three-entry digest table plus a map
lookup in `ManifestFor`, a cold config path that runs once per manifest
build. It adds no Cypher, no worker claims, no leases, no batching, no
concurrency knobs, and no runtime or Compose/Helm settings.

No-Regression Evidence: baseline package suite on clean `origin/main`
(`77cba71c95`, detached worktree, same host) passes in 3.584s; the same
suite with the pins plus two new tests passes in 3.499s, and with `-race`
in 5.0s. The only added production work per manifest build is one map
lookup; manifest construction is not on any query, ingest, or graph-write
path, so no hot-path statement changes shape or cardinality. No benchmark
delta is claimed because no measured path exists to improve.

No-Observability-Change: no metric, span, log, or status field is added,
renamed, or removed. The one new string is a fail-closed error return for
an unpinned component id, surfaced to the caller, not emitted as telemetry.

## Re-pin after the seed publish (merge 01c8781a, docker-publish run 40892918102)

The seed subcommand change (`seed.go`, per-binary dispatch, three mains)
advanced scanner inputs past anchor `92744768`, so
`verify-scanner-digest-pins.sh` reports STALE on current main. This bump
moves the anchor to `01c8781a71b75e2c9a19611b40b0140ec68de1fe` and the three
pins to the `sha-01c8781a...` tags published from that merge:

- image `fd712231...d22fe`, sbom `484e5aa4...debe0`,
  osruntime `fbca5bab...07b` (full hex in `manifest.go`).
- Each digest read back with `crane digest` and its build attestation
  re-verified with `gh attestation verify oci://... -R eshu-hq/eshu`
  (exit 0; negative control on a bogus digest fails MANIFEST_UNKNOWN).
- Same no-perf/no-observability reasoning as above: only hex constants
  change; no code path, query, or telemetry is touched.

No-Regression Evidence (seed re-pin 01c8781a): component suite 7.567s on
clean main (`73418463ed`, throwaway worktree, same host) vs 7.4-7.5s with
the new pins; constants-only change, manifest construction off any query,
ingest, or graph-write path.

No-Observability-Change (seed re-pin 01c8781a): no metric, span, log, or
status field added, renamed, or removed by the new pin values.

## Re-pin after the seed-to-admission merge (merge 6ce31d8b9d, docker-publish run 40963555579)

The seed-to-admission move (#9539) advanced scanner inputs past anchor
`01c8781a`, so `verify-scanner-digest-pins.sh` reports STALE on current
main. This bump moves the anchor to
`6ce31d8b9d88843c869468d65f345798a96dd42b` and the three pins to the
`sha-6ce31d8b9d...` tags published from that merge:

- image `ffddf249...01d6c82f5`, sbom `f5acc7cb...498988e70`,
  osruntime `fd6ed407...6dbc575e8` (full hex in `manifest.go`).
- Each digest read back with `crane digest` and its build attestation
  re-verified with `gh attestation verify oci://... -R eshu-hq/eshu`,
  both with the default and the SPDX predicate (exit 0 on all six).
- Same no-perf/no-observability reasoning as above: only hex constants
  change; no code path, query, or telemetry is touched. The mirrored
  in-repo chart values in the runtime contract test move with the pins;
  the live chart carries no scanner seed.image entries.

No-Regression Evidence (seed re-pin 6ce31d8b9d): component suite 7.548s
on clean main (`300ec193b6`, throwaway worktree, same host) vs 7.542s
with the new pins; constants-only change, manifest construction off any
query, ingest, or graph-write path. Backend/version: no backend touched
(unit suite, no graph backend); input shape: three pin constants plus
anchor line; queue/row counts: none (no queue involved).

No-Observability-Change (seed re-pin 6ce31d8b9d): no metric, span, log,
or status field added, renamed, or removed by the new pin values; seed
and admission failures keep the existing defect-only startup-error path
with no new telemetry.
