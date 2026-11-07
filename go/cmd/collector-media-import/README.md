# Collector Media Import

## Purpose

This binary drains one batch of operator-supplied, reviewed audio artifacts
through `document/media` transcript extraction and commits the resulting
envelopes with the normal ingestion store. It is the first disabled-by-default
in-tree caller of a dormant document-import capability for [#8879](https://github.com/eshu-hq/eshu/issues/8879):
it creates the non-test call edge the gate-2 checklist needs for `document/media`
without enabling any hosted path.

## Ownership boundary

This binary owns explicit-artifact transcript drains only: list a directory,
extract each file, commit each generation, exit. It does not own discovery,
archives, providers, scheduling, polling, status serving, or any other
document-import capability (`document/ocr`, `document/export`, the `preflight/*`
heads). Runtime enablement, sandboxing, and dependency review stay caller-owned
per the `document/media` contract; this binary is the caller that carries them for
its own drain.

What it explicitly does not do: deploy itself (no chart, no IaC reference, no
image-pin change), run on a schedule (one-shot, then exit), fall back to
another engine (a missing pin is a startup error, never a quiet substitution),
or accept non-QA tiers (only `qa` enables injection; no production caller beyond
the QA tier is claimed).

## Exported surface

Package `main`; nothing is imported by other packages.

- `Config`, `loadMediaImportConfig` — startup configuration from the environment.
- `dirSource`, `newDirSource` — `collector.Source` adapter: one generation per
  distinct artifact, identity from content SHA-256.
- `drainOnce` — one-shot commit loop over the source.
- `buildDrain`, `buildEngine` — pinned backend verification and drain wiring.
- `runSeed`, `runSeedCommand` — `collector seed` dispatch: digest-ref
  validation plus the caller binary, embedded `manifest.yaml`, and pinned
  whisper engine payload copy (`engine/` with `bin/` + `models/`, every
  file SHA-256-verified during a streaming copy, plus `pin-manifest.yaml`
  and the musl loader path file `lib-path`).
- `serveStdioClaim`, `runSDKStdioCommand`, `captureCommitter`, `ackResult` —
  `--sdk-stdio` grant-checked claim adapter: one host claim in, one SDK
  result out, existing drain unchanged in between, terminal `grant_denied`
  ack on revoked grants.

## Satellite seed (Event-B)

`manifest.yaml` is the component package the host admits
(`dev.eshu.collector.media_import` 0.1.0, process adapter over
`collector-sdk/v1alpha1`, seed image digest-pinned, emitted families
`documentation_document` 1.0.0 plus `documentation_section` 1.1.0).
`Dockerfile.seed` rebuilds the transcription satellite runtime from its
pinned stages and adds the seed contract surface: `/usr/local/bin/collector`
(a symlink, so the existing `ENTRYPOINT` path keeps working) and
`/manifest.yaml`. The local proof build is linux/arm64; promotion re-pins
the manifest entry to the pushed digest.

`collector seed --dest <dir> --image-ref <repo@sha256:…>` materializes the
seed payload (binary as `collector`, manifest as `manifest.yaml` with the
passed digest stamped into the artifact entry for the seed runner's
platform, engine as `engine/` with `bin/whisper-cli` plus
`models/ggml-base.en.bin`, every file SHA-256-verified during a streaming
copy, plus `pin-manifest.yaml` and the musl loader path file `lib-path`)
and fails closed on any non-digest reference, any reference outside this
component's own seed repository, a manifest with no entry for the runner's
platform, or any copy or digest failure — removing the destination so no
partial payload survives. Sibling platform slots are left byte-identical.
The env pins move to the runtime dir at the IaC step; the seed writes
exactly the pinned set (no seeded file unpinned, no pin unseeded).

`--sdk-stdio` serves one host claim: the claim's generation drains through
the existing source and drain into an in-memory capture, the envelopes
project onto the SDK wire shape (self-checked against the embedded manifest
contract) when the activation's live producer grants authorize every emitted
kind, and the ack echoes the claim verbatim with one complete status row. A
grant denial acks terminal with `grant_denied`; the satellite opens no
database and any other failure exits nonzero with no ack on stdout.

See `doc.go` for the full godoc contract.

## Dependencies

- `go/internal/collector/document/media` — extraction and redaction; envelopes
  are committed unchanged.
- `go/internal/collector/document/media/whispercpp` — the only engine backend;
  pinned binary and model digest, verified at startup. WAV-only: any other
  format drains as a skipped observation and never reaches the engine.
- `go/internal/collector/preflight/media` — metadata-only media preflight,
  reached transitively through `media.Extract`.
- `go/internal/collector` — `Source` and `Committer` boundaries.
- `go/internal/scope` — `documentation` collector kind, `documentation_source`
  scope kind, snapshot trigger.
- `go/internal/storage/postgres` — the normal ingestion store committer.

## Configuration

| Variable | Required when enabled | Role |
| --- | --- | --- |
| `ESHU_MEDIA_IMPORT_ENABLED` | — | Case-insensitive `true` opts in; anything else exits 0 with zero commits. |
| `ESHU_MEDIA_IMPORT_TIER` | yes | Must be exactly `qa`: QA-tier-only engine injection (QA tier, #8879). |
| `ESHU_MEDIA_IMPORT_ARTIFACT_DIR` | yes | Absolute directory of reviewed audio files. |
| `ESHU_MEDIA_IMPORT_WHISPER_BINARY_PATH` | yes | Absolute whisper-cli path; never resolved via PATH. |
| `ESHU_MEDIA_IMPORT_WHISPER_BINARY_SHA256` | yes | Pinned hex digest of the whisper-cli executable. |
| `ESHU_MEDIA_IMPORT_WHISPER_MODEL_PATH` | yes | Absolute pinned ggml model path. |
| `ESHU_MEDIA_IMPORT_WHISPER_MODEL_SHA256` | yes | Pinned hex digest of the model file. |
| `ESHU_MEDIA_IMPORT_WHISPER_ENGINE_VERSION` | yes | Reviewed whisper.cpp release the backend records, e.g. `v1.9.5`. |
| `ESHU_MEDIA_IMPORT_SCOPE_PREFIX` | no | Default `media-import-transcript`. |
| `ESHU_MEDIA_IMPORT_SOURCE_SYSTEM` | no | Default `media_import`. |
| `ESHU_MEDIA_IMPORT_MAX_ARTIFACTS` | no | Per-run artifact bound; default 1024, must stay positive. |

Digests accept an optional `sha256:` prefix. Run bounds stay at the engine
defaults, which sit within the design 1737 section 6 ceilings; raising one
needs a design update and security review. Only WAV drains to the engine;
every other input format stays off by default and records a skipped
observation.

## Telemetry

- Metrics: `eshu_dp_media_transcript_extractions_total` and
  redacted-segment counts — emitted on this path through the backend's
  `Extract` wrapper, which is the sole emitter (the raw engine call carries
  only the engine-run instruments). The wrapper is new in this lane; the
  binary's own files emit no metric of their own.
- Spans: `SpanMediaTranscriptExtract` per artifact plus the standard
  commit-level spans.
- Logs: JSON lines; scan counts at startup, one line per committed generation,
  drain totals at exit. No transcript text ever reaches logs or spans: the
  engine contract keeps raw text out of errors, and redaction runs before
  envelopes.

## Performance Evidence

No-Regression Evidence: this lane adds a new binary and registry/docs rows;
it modifies no shared production code path. The focused suites for every
touched package stay green (`go test ./cmd/collector-media-import/ -count=1`;
whispercpp `Extract` wrapper tests; `envregistry`: pass). Backend: local `go`
toolchain; no Neo4j/Postgres touched by these unit paths. Input shape:
operator-supplied reviewed audio bounded by `maxInputBytes+1` per read with
single-file peak residency (64 MiB + 1 byte at defaults); terminal counts are
the drain's own generation/envelope totals, committed through the unchanged
ingestion store. Gate impact is nil by construction: the binary runs only when
explicitly enabled on the QA tier, and disabled start exits 0 with zero
commits, zero DB connections, and zero envelopes.

Observability Evidence: extraction status, region counts, and redaction
counts stay covered by `eshu_dp_media_transcript_extractions_total` and
`eshu_dp_media_transcript_redacted_segments_total` plus the
`media.transcript_extract` span, all counted by the `Extract` wrapper with
caller-provided instruments (row recorded in
`docs/public/observability/telemetry-coverage-collectors.md`); commit
outcomes by the standard ingestion-store commit path; the caller binary's
own files emit no metric of their own.

Follow-up (digest-pin normalization, #10039 review thread).
No-Regression Evidence: `loadHexDigest` now returns the normalized lowercase
digest instead of the raw `sha256:`-prefixed/uppercase input, and library
pins
store the stripped form; a return-value-only change with no control-flow, I/O,
Cypher, queue, or concurrency delta. Baseline `origin/main` (`0cb384ca`);
after measurement at this HEAD the focused suites stay green (`go test
./cmd/collector-media-import/ ./cmd/collector-document-import/ -count=1`).
Backend: local `go` toolchain; no Neo4j/Postgres touched. Input shape:
operator-supplied 64-hex-character pin strings; terminal counts unchanged.

No-Observability-Change: no new metric, label, span, or log line; the
extraction and commit instruments above are untouched.

### Seed and stdio paths (Event-B, this PR)

No-Regression Evidence: the `seed` and `--sdk-stdio` entry points are new
dispatch branches; the drain loop, whisper backend calls, queue/lease
handling, and batching are untouched. Baseline is the pre-change focused
suite (`go test ./cmd/collector-media-import/ -count=1`: green, 42 pass
lines, ~0.01 s); after measurement at this HEAD the same suite stays green
with unchanged timings, plus the new seed/stdio suites (per-arch stamp,
sibling-untouched,
wrong-repo and missing-platform refusals, claim ack and grant-denied ack)
and the four gated seed-image builds (11-library set, GATE PASS per arch).
Backend: pinned whisper.cpp v1.9.5 via the apk floors; linux/arm64 + amd64
builds. Input shape: seed payload is the caller binary plus manifest under
fixed names into the operator `--dest` dir (0750); stdio serves exactly one
host claim over the inherited pipe. Terminal counts are the drain's own
generation/envelope totals. Gate impact is nil by construction: both paths
run only under their explicit subcommands.

No-Observability-Change: no new metric, label, span, or log key on either
path; `seed` writes files only, `--sdk-stdio` acks over stdout, and the
drain's existing extraction/commit signals are unchanged. Seed files are
covered by the `telemetry-coverage-collectors.md` drain row.

### Seeded engine payload (this PR)

No-Regression Evidence: `seed` additionally copies the pinned engine tree
(`bin/whisper-cli`, `models/ggml-base.en.bin`) into `<dest>/engine/`,
streaming each file while hashing against its pin; any mismatch removes the
payload. Baseline is the payload without engines (every claim fails at pin
verification); after measurement at this HEAD the focused suite stays green
(`go test ./cmd/collector-media-import/ -count=1`) including tamper,
missing-source, and pin-drift RED/GREEN pairs. The copy helper is the same
one proven live for OCR; the whisper container end-to-end stays NOT_CHECKED.
Backend: pinned whisper.cpp v1.9.5; linux/arm64 + amd64 pin tables. Input
shape: seed-image absolute paths under the fixed `engine/` names into the
operator `--dest` dir (0750, engine subdirs 0750, payload files 0444/0555).
Terminal counts are the drain's own generation/envelope totals; the claim
decode/validate/commit path is untouched. Gate impact is nil by
construction: the copy runs only inside `seed`.

No-Observability-Change: `engine.go` emits no metric, label, span, or log
line; it is covered by the same `telemetry-coverage-collectors.md` drain
row, and extraction/commit signals stay the existing
`eshu_dp_media_transcript_*` set.

No-Regression Evidence: the seed additionally plants relative soname links
derived from each `lib/` ELF's DT_SONAME (same helper as the OCR lane),
and re-seed is idempotent via chmod-retry with mode re-assertion.
Baseline is the link-less payload; after measurement the focused suite
stays green including the guard table and reseed tests. No hot path is
touched: the seeder runs once per pod start.

No-Observability-Change: still no metric, label, span, or log line; the
link derivation adds no signal.

## Gotchas / invariants

- Disabled means exit 0 with zero commits: the binary is built but never
  deployed, and startup requires the enable flag plus the QA tier, the dir,
  and every pin.
- QA-tier-only: `ESHU_MEDIA_IMPORT_TIER` must be exactly `qa`; any other
  value fails closed. No production caller beyond the QA tier is claimed.
- WAV-only: only WAV reaches the engine. Every other format drains as the
  honest skipped observation the `media` package already emits; the engine is
  never invoked for it.
- One generation per distinct content hash: duplicate bytes commit once, and
  re-runs over identical bytes name the same generation.
- Symlinks, directories, devices, oversize files, and entries past the cap
  are skipped and counted, never committed and never fatal.
- A cancelled context stops the drain before the next commit: committed
  generations stay committed, unstarted ones never begin.
- Artifact file names persist verbatim in the document `Title` and in scope
  metadata (`artifact_name`); only the source URI passes through the
  redacting `safeSourceURI`. Use inert file names: redaction applies to file
  contents, not to names.
- A file rewritten between scan and drain fails the run instead of committing
  under a stale content identity.
- Gate status: this wiring records checklist item (ii) only. It flips neither
  gate and changes no stage; hosted activation stays disabled.

## Related docs

- Gate record: [#9382](https://github.com/eshu-hq/eshu/issues/9382).
- Design rationale: `docs/internal/design/9728-ocr-engine-backend.md` (OCR
  lane) and the whisper.cpp backend README for the transcript lane.
- Arbiter ruling for this wiring: `docs/internal/evidence/8879-caller-choice-arbiter-ruling.md`.
- Runbook for future cutover work: the satellite collector runbook merged for
  the extracted collectors.
