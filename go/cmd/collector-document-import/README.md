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

What it explicitly does not do: deploy itself (no chart, no IaC reference, no
image-pin change), run on a schedule (one-shot, then exit), or fall back to
another engine (a missing pin is a startup error, never a quiet substitution).

## Exported surface

Package `main`; nothing is imported by other packages.

- `Config`, `loadImportConfig` — startup configuration from the environment.
- `dirSource`, `newDirSource` — `collector.Source` adapter: one generation per
  distinct artifact, identity from content SHA-256.
- `drainOnce` — one-shot commit loop over the source.
- `buildDrain`, `buildEngine` — pinned backend verification and drain wiring.

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
| `ESHU_DOC_IMPORT_ENABLED` | — | `true` opts in; anything else exits 0 with zero commits. |
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
