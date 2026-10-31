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

## Re-pin after the retry fix (merge 768eeed282, docker-publish run 41000640165)

The retry fix (#9596) advanced scanner inputs past anchor `6ce31d8b9d`,
so `verify-scanner-digest-pins.sh` reports STALE on current main. This
bump moves the anchor to
`768eeed2829b149609c17db7aa8d7788147c0173` and the three pins to the
`sha-768eeed282...` tags published from that merge:

- image `f92fabfc...0e7bd372`, sbom `cf0e10d6...4906c9ecd`,
  osruntime `3dbca86b...f60821e31` (full hex in `manifest.go`).
- Each digest read back with `crane digest` and its build attestation
  re-verified with `gh attestation verify oci://... -R eshu-hq/eshu`,
  both with the default and the SPDX predicate (exit 0 on all six).
- Review feedback removed the #9600 fold from this PR: any
  `runner_test.go` change reads as a scanner input newer than the
  anchor post-merge and would re-stale the gate, so the literal-60
  assertion stays queued for the next functional scanner change
  (#9600 stays closed with its pointer). Same
  no-perf/no-observability reasoning as above: only hex constants
  change; no code path, query, or telemetry is touched. The mirrored
  in-repo chart values in the runtime contract test move with the
  pins; the live chart carries no scanner seed.image entries.

No-Regression Evidence (retry re-pin 768eeed282): component suite
7.971s with the prior pins on the same host vs 7.805s with the new
pins; `go test ./internal/collector/scannerworker/...
./internal/runtime/ -count=1` green at HEAD, and
`scripts/verify-scanner-digest-pins.sh` PASS (3 components at
sha-768eeed2829b, inputs fresh). Constants-only change, manifest
construction off any query, ingest, or graph-write path.
Backend/version: no backend touched (unit suite, no graph backend);
input shape: three pin constants plus anchor line; queue/row counts:
none (no queue involved).

No-Observability-Change (retry re-pin 768eeed282): no metric, span,
log, or status field added, renamed, or removed by the new pin
values; seed and admission failures keep the existing defect-only
startup-error path with no new telemetry.

## Re-pin after the ASCII identity fold (merge 17e42456aa, docker-publish run 41099864459)

The ASCII-letters-only identity fold (#9619, commit `a6af7e40dc`)
changed `sbomgenerator/envelope.go` and one sbomgenerator test after
anchor `768eeed282`, so `verify-scanner-digest-pins.sh` reported STALE
on main (the pins still matched their tags; they were only stale). This
bump moves the anchor to
`17e42456aaa3a411cb429babd3dcf2b63bd07941` and the three pins to the
`sha-17e42456aa...` tags published from that merge. It is the newest
first-parent main commit with all three immutable scanner tags, and no
scanner input file changed on main between it and the base of this
change:

- image `2c9269e9...7b423815`, sbom `2d3b72aa...b61ee43da`,
  osruntime `6ff09b67...955e2b574` (full hex in `manifest.go`).
- Each digest read back with `crane digest` and its build attestation
  verified with `gh attestation verify oci://... -R eshu-hq/eshu`, with
  the default and the SPDX predicate (exit 0 on all six).
- Why the images include the fold: `a6af7e40dc` is an ancestor of
  `17e42456aa`, and each image's attestation records
  `sourceRepositoryDigest` `17e42456aaa3a411cb429babd3dcf2b63bd07941`
  on `refs/heads/main`. Run 41099864459 finished with every job
  successful, including `verify-reproducibility`.
- The mirrored in-repo chart values in the runtime contract test move
  with the pins. The deploy repo does NOT move with this change. At
  eshu-deploy `origin/main` 64bf912e0d4bf79f08eaa0149d3978c1d9b2c451,
  `helm/eshu/ci/scanner-activations.values.yaml` (lines 41, 77, 108)
  and its contract test
  `go/internal/runtime/helm_scanner_activations_contract_test.go`
  (lines 23-25) still hold the old digests `f92fabfc...`,
  `cf0e10d6...`, and `3dbca86b...`. The default chart values carry no
  scanner `seed.image` entries (commented examples only). Once an
  environment runs an eshu build that carries the pins above, an
  environment that applies the scanner values file with `-f` seeds
  digests that build no longer admits, and admission fails closed
  for the scanner components until the deploy files are bumped to
  the same pins. Merging this change alone does not alter admission.
  The deploy bump is a companion change owned outside this PR.

No-Regression Evidence (fold re-pin 17e42456aa): component suite, same
host and load, alternating first mover: base 30.022s and 28.854s vs
this change 27.349s and 29.914s (about 29.4s vs 28.6s mean). `go test
./internal/collector/scannerworker/... ./internal/runtime/ -count=1`
green, and `scripts/verify-scanner-digest-pins.sh` PASS (3 components
at sha-17e42456aaa3, inputs fresh) after exiting 1 STALE at the base.
Constants-only change, manifest construction off any query, ingest, or
graph-write path. Backend/version: no backend touched (unit suite, no
graph backend); input shape: three pin constants plus anchor line;
queue/row counts: none (no queue involved).

No-Observability-Change (fold re-pin 17e42456aa): no metric, span,
log, or status field added, renamed, or removed by the new pin values;
seed and admission failures keep the existing defect-only startup-error
path with no new telemetry.

## Re-pin past #9657 GHCR-cache drift (merge dec23a17d9, docker-publish run 41156813471)

#9657 ("Move the Docker build cache to a GHCR registry cache",
commit e94f6fca44) rewrote the `build-and-push-scanner-images` job body in
`.github/workflows/docker-publish.yml` (gha cache to
`type=registry,ref=ghcr.io/eshu-buildcache:...`, push-only write). That job
body is a watched non-Go input of the #9503 content-hash section, so the
gate reports STALE past anchor `17e42456aaa3a411cb429babd3dcf2b63bd07941`
on any merge tree at or after `dec23a17d9` (first seen as the single red
on PR #9684's wave). The `dec23a17d9` merge-head publish (run 41156813471)
pushed all three scanner images; the `sha-dec23a17d9...` tags carry new
index digests. This bump moves the anchor to
`dec23a17d9cf6aa196f1ec66c3570a441164c867` and the three pins to those
tags:

- image `552d9087...4bda09`, sbom `fcd10e5e...95fea1`,
  osruntime `40d93b0e...4e675e` (full hex in `manifest.go`).
- Each index digest read back with `crane digest` against its
  `sha-dec23a17d9...` tag, and each subject carries an attestation bundle
  on the attestations API (parity with the currently pinned digests, which
  also list one bundle each); the authoritative `gh attestation verify`
  runs in the gate itself on the PR wave (local gh 2.46.0 lacks the
  `attestation` subcommand, so local proof delegates that step to CI and
  states it here instead of claiming it).
- `git log dec23a17d9..origin/main` over the script's scanner inputs is
  empty, so the new anchor stays green on current main. No fold-ins: only
  the anchor line, the three pins, their mirrored test copies, and this
  note change.

No-Regression Evidence (re-pin dec23a17d9): RED-first —
`TestManifestArtifactPinsPublishTimeDigestContract` with the new
expectations failed against the old pins, then passed with the fix;
`TestScannerActivationSeedImagesMatchPinnedManifests` failed on the old
mirrored YAML the same way, then passed.
Constants-only change, manifest construction off any query, ingest, or
graph-write path.

No-Observability-Change (re-pin dec23a17d9): no metric, span,
log, or status field added, renamed, or removed by the new pin values;
seed and admission failures keep the existing defect-only startup-error
path with no new telemetry.
