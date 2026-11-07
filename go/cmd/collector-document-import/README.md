# Collector Document Import

## Purpose

This binary drains one batch of operator-supplied, reviewed image artifacts
through `document/ocr` extraction and commits the resulting envelopes with the
normal ingestion store. It is the first disabled-by-default in-tree caller of
a dormant document-import capability for [#8879](https://github.com/eshu-hq/eshu/issues/8879):
it creates the non-test call edge the gate-1 checklist needs without enabling
any hosted path.

## Ownership boundary

This binary owns explicit-artifact OCR drains only: list a directory, extract
each file, commit each generation, exit. It does not own discovery, archives,
providers, scheduling, polling, status serving, or any other document-import
capability (`document/media`, `document/export`, the `preflight/*` heads).
Runtime enablement, sandboxing, and dependency review stay caller-owned per
the `document/ocr` contract; this binary is the caller that carries them for
its own drain.

What it explicitly does not do: deploy itself (no chart, no IaC reference; the
satellite activation lives in the deploy chart, not here), run on a schedule
(one-shot, then exit), or fall back to another engine (a missing pin is a
startup error, never a quiet substitution).

## Exported surface

Package `main`; nothing is imported by other packages.

- `Config`, `loadImportConfig` — startup configuration from the environment.
- `dirSource`, `newDirSource` — `collector.Source` adapter: one generation per
  distinct artifact, identity from content SHA-256.
- `drainOnce` — one-shot commit loop over the source.
- `buildDrain`, `buildEngine` — pinned backend verification and drain wiring.
- `runSeedCommand`, `seedComponent`, `parseSeedArgs` (`seed.go`) plus
  `enginePayloadForPlatform`, `seedEnginePayload` (`engine.go`) — the
  satellite seed branch: copy the caller, the installed manifest (stamped
  with the digest-pinned seed image), and the pinned tesseract engine
  payload (`engine/` with `bin/` + flat `lib/` + `tessdata/`, every file
  SHA-256-verified during a streaming copy, plus `pin-manifest.yaml` and
  the musl loader path file `lib-path`).
- `serveSDKStdio`, `captureCommitter` (`sdkstdio.go`) — the satellite claim
  adapter: one host claim from stdin, existing verification plus the
  untouched drain, grant-checked commit into the SDK ack, served against
  the embedded manifest (`manifest.yaml` compiled into the caller).

See `doc.go` for the full godoc contract.

## Satellite activation (Event-B)

The seed image (`Dockerfile.seed`, provenance in `image-provenance.md`)
carries the caller at `/usr/local/bin/collector` (plus the legacy
`/usr/local/bin/eshu-collector-document-import` and `/collector` symlinks)
and the component manifest at `/manifest.yaml`. The activation chart runs
`/collector seed --dest <runtimeDir> --image-ref <seed.image>` as a seed
init container, then launches `<runtimeDir>/collector --sdk-stdio` per
claim through the component-extension host.

- `seed` writes `<dest>/collector` (executable),
  `<dest>/manifest.yaml` (seed image stamped in), and `<dest>/engine/`
  (`bin/tesseract`, every `ESHU_DOC_IMPORT_TESSERACT_LIBRARIES` pin flat
  under `lib/`, `tessdata/eng.traineddata`, plus `pin-manifest.yaml` and
  the musl loader path file `lib-path`), failing closed on any reference
  that is not digest-pinned (`repo@sha256:<64 hex>`) and removing the
  destination on any copy or digest failure, so no partial payload survives.
  The env pins move to the runtime dir at the IaC step; the seed writes
  exactly the pinned set (no seeded file unpinned, no pin unseeded).
- `--sdk-stdio` reads one bounded host claim, verifies the environment and
  engine pins exactly like the one-shot path, drains the activation
  artifact dir, and acks `complete` (facts), `unchanged` (empty drain), or
  `terminal` (deterministic grant denial). Facts are bound to the claimed
  scope and generation on the wire; content identity (stable key, payload)
  travels unchanged. The durable commit stays host-side. The claim path
  serves the embedded manifest, so `/manifest.yaml` stays only as the seed
  source inside the seed image.
- Grant source is the activation registry (`ESHU_COMPONENT_HOME`), which
  the chart reconcile populates before install. An unreadable or empty
  grant set denies core-owned emission fail-closed, mirroring the host's
  emission recheck. No new environment variables were added for either
  branch.

See `doc.go` for the full godoc contract.

## Dependencies

- `go/internal/collector/document/ocr` — extraction and redaction; envelopes
  are committed unchanged.
- `go/internal/collector/document/ocr/tesseract` — the only engine backend;
  pinned binary, libraries, and model digest, verified at startup.
- `go/internal/collector/preflight/picture` — metadata-only image preflight,
  reached transitively through `ocr.Extract`.
- `go/internal/collector` — `Source` and `Committer` boundaries.
- `go/internal/scope` — `documentation` collector kind, `documentation_source`
  scope kind, snapshot trigger.
- `go/internal/storage/postgres` — the normal ingestion store committer.

## Configuration

| Variable | Required when enabled | Role |
| --- | --- | --- |
| `ESHU_DOC_IMPORT_ENABLED` | — | Case-insensitive `true` opts in; anything else exits 0 with zero commits. |
| `ESHU_DOC_IMPORT_TIER` | yes | Must be exactly `qa`: QA-tier-only engine injection (QA tier, #8879). |
| `ESHU_DOC_IMPORT_ARTIFACT_DIR` | yes | Absolute directory of reviewed image files. |
| `ESHU_DOC_IMPORT_TESSERACT_BINARY_PATH` | yes | Absolute tesseract path; never resolved via PATH. |
| `ESHU_DOC_IMPORT_TESSERACT_BINARY_SHA256` | yes | Pinned hex digest of the binary. |
| `ESHU_DOC_IMPORT_TESSERACT_LIBRARIES` | yes | `path=digest` pins, `;`-separated; at least one. |
| `ESHU_DOC_IMPORT_TESSERACT_TESSDATA_DIR` | yes | Absolute tessdata directory. |
| `ESHU_DOC_IMPORT_TESSERACT_MODEL_SHA256` | yes | Pinned hex digest of `eng.traineddata`. |
| `ESHU_DOC_IMPORT_TESSERACT_ENGINE_VERSION` | yes | Reviewed release, e.g. `5.5.0`. |
| `ESHU_DOC_IMPORT_SCOPE_PREFIX` | no | Default `document-import-ocr`. |
| `ESHU_DOC_IMPORT_SOURCE_SYSTEM` | no | Default `document_import`. |
| `ESHU_DOC_IMPORT_MAX_ARTIFACTS` | no | Per-run artifact bound; default 1024, must stay positive. |

Digests accept an optional `sha256:` prefix. Caps come from the pinned
`tesseract` defaults; raising one needs a design update.

## Telemetry

- Metrics: `eshu_dp_ocr_extractions_total` and redacted-region counts — emitted
  on this path through the backend's `Extract` wrapper, which is the sole
  emitter (the raw engine call carries no instruments). No-Observability-Change:
  existing signals, no new metric.
- Spans: `SpanOCRExtract` per artifact plus the standard commit-level spans.
- Logs: JSON lines; scan counts at startup, one line per committed generation,
  drain totals at exit. No OCR text ever reaches logs or spans: the engine
  contract keeps raw text out of errors, and redaction runs before envelopes.

## Performance Evidence

No-Regression Evidence: this PR adds a new binary and registry/docs rows; it
modifies no shared production code path. Baseline is `origin/main`
`6ed93565a5` (no such binary); after measurement at branch HEAD, the focused
suites for every touched package stay green with unchanged timings
(`go test ./cmd/collector-document-import/ -count=1`: ~0.01 s;
`document/...` and `preflight/...` suites: same pass, no delta;
`envregistry`: pass). Backend: linux/arm64 local `go` toolchain; no
Neo4j/Postgres touched by these unit paths. Input shape: operator-supplied
reviewed images bounded by `maxInputBytes+1` per read with single-file peak
residency (25 MiB + 1 byte at defaults); terminal counts are the drain's own
generation/envelope totals, committed through the unchanged ingestion store.
Gate impact is nil by construction: the binary runs only when explicitly
enabled (`ESHU_DOC_IMPORT_ENABLED=true`), and disabled start exits 0 with
zero commits, zero DB connections, and zero envelopes.

No-Observability-Change: existing `eshu_dp_ocr_extractions_total`,
`SpanOCRExtract`, and commit-level spans only; no new metric, label, or span.
Row recorded in `docs/public/observability/telemetry-coverage-collectors.md`.

### Seed and stdio paths (Event-B, this PR)

No-Regression Evidence: the `seed` and `--sdk-stdio` entry points are new
dispatch branches; the drain loop, backend calls, queue/lease handling, and
batching are untouched. Baseline is the pre-change focused suite
(`go test ./cmd/collector-document-import/ -count=1`: green, 56 pass lines,
~0.01 s); after measurement at this HEAD the same suite stays green with
unchanged timings,
plus the new seed/stdio suites (per-arch stamp, sibling-untouched,
wrong-repo and missing-platform refusals, claim ack and grant-denied ack)
and the four gated seed-image builds (22-library set, GATE PASS per arch).
Backend: pinned tesseract 5.5.0 via the apk floors; linux/arm64 + amd64
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
(`bin/tesseract`, 22 flat libs, `tessdata/eng.traineddata`) into
`<dest>/engine/`, streaming each file while hashing against its pin; any
mismatch removes the payload. Baseline is the payload without engines (every
claim fails at pin verification); after measurement at this HEAD the focused
suite stays green (`go test ./cmd/collector-document-import/ -count=1`)
including tamper, missing-source, and pin-drift RED/GREEN pairs, and a live
alpine:3.21 shim acks `unchanged` with zero facts on an empty dir (identical
ack with `manifest.yaml` deleted). Backend: pinned tesseract 5.5.0;
linux/arm64 + amd64 pin tables. Input shape: seed-image absolute paths under
the fixed `engine/` names into the operator `--dest` dir (0750, engine
subdirs 0750, payload files 0444/0555). Terminal counts are the drain's own
generation/envelope totals; the claim decode/validate/commit path is
untouched except the manifest byte source. Gate impact is nil by
construction: the copy runs only inside `seed`.

No-Observability-Change: `engine.go` emits no metric, label, span, or log
line; it is covered by the same `telemetry-coverage-collectors.md` drain
row, and extraction/commit signals stay the existing `eshu_dp_ocr_*` set.

No-Regression Evidence: the seed additionally plants relative soname links
derived from each `lib/` ELF's DT_SONAME (digest-verified bytes stay the
trust root; unsafe names, collisions, and multi-SONAME fail closed), and
re-seed is idempotent via chmod-retry with mode re-assertion. Baseline is
the link-less payload (loader `No such file`); after measurement the
focused suite stays green including the guard table and reseed tests, and
the live alpine shim resolves `tesseract 5.5.0` via the
loader path file without `LD_LIBRARY_PATH`. No hot path is touched: the
seeder runs once per pod start.

No-Observability-Change: still no metric, label, span, or log line; the
link derivation adds no signal.

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
instruments above are untouched.

## Gotchas / invariants

- Disabled means exit 0 with zero commits: the binary is built but never
  deployed, and startup requires the enable flag plus dir plus every pin.
- One generation per distinct content hash: duplicate bytes commit once, and
  re-runs over identical bytes name the same generation.
- Symlinks, directories, devices, oversize files, and entries past the cap
  are skipped and counted, never committed and never fatal.
- A cancelled context stops the drain before the next commit: committed
  generations stay committed, unstarted ones never begin.
- Non-image files commit the honest skipped observation the `ocr` package
  already emits; the engine is never invoked for them.
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
- Design rationale: `docs/internal/design/9728-ocr-engine-backend.md`.
- Arbiter ruling for this wiring: `docs/internal/evidence/8879-caller-choice-arbiter-ruling.md`
  (to be filed with the wiring PR).
- Runbook for future cutover work: the satellite collector runbook merged for
  the extracted collectors.
