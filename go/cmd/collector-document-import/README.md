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

- Metrics: `eshu_dp_ocr_*` — emitted by the extraction path, unchanged.
- Spans: the standard collector observe spans around each commit.
- Logs: JSON lines; scan counts at startup, one line per committed generation,
  drain totals at exit. No OCR text ever reaches logs or spans: the engine
  contract keeps raw text out of errors, and redaction runs before envelopes.

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
- Gate status: this wiring records checklist item (ii) only. It flips neither
  gate and changes no stage; hosted activation stays disabled.

## Related docs

- Gate record: [#9382](https://github.com/eshu-hq/eshu/issues/9382).
- Design rationale: `docs/internal/design/9728-ocr-engine-backend.md`.
- Arbiter ruling for this wiring: `docs/internal/evidence/8879-caller-choice-arbiter-ruling.md`
  (to be filed with the wiring PR).
- Runbook for future cutover work: the satellite collector runbook merged for
  the extracted collectors.
