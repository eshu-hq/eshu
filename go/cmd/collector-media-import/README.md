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
| `ESHU_MEDIA_IMPORT_ENABLED` | — | `true` opts in; anything else exits 0 with zero commits. |
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
