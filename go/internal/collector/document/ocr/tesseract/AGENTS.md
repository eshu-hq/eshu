# AGENTS.md - collector/document/ocr/tesseract guidance for LLM assistants

## Read first

1. `go/internal/collector/document/ocr/tesseract/README.md`
2. `docs/internal/design/9728-ocr-engine-backend.md`
3. `go/internal/collector/document/ocr/README.md` and `extract.go`
4. `docs/internal/design/1737-visual-media-documentation-ingestion.md`

## Invariants

- Out of process only. Do not link libtesseract or any OCR library into Go,
  and never call a hosted OCR or vision service (design 1737).
- Exec the absolute, digest-pinned binary with the fixed argument vector. No
  shell, no PATH lookup, no caller-supplied arguments, no inherited
  environment.
- Run every pre-exec check before `run`: context, pinned-file identity, byte
  cap, format magic, header dimensions, pixel cap.
- Fail closed. Any non-success outcome returns no regions.
- Subprocess stdout and stderr never reach an error, log, span, or metric.
  Error detail uses only sizes, caps, exit codes, signals, and formats.
- Regions are lines. Redaction in `ocr.Extract` depends on it.
- `Engine.Extract` calls `ocr.Extract` unchanged. Do not add redaction or fact
  logic here.
- Labels stay closed: `outcome` from `errors.go`, `status` from
  `telemetry.go`. Add an `Outcome` only with a coverage-doc and metrics-doc
  update.
- Keep OCR default-off. Wiring a caller (ingester, collector, CLI) is a
  separate reviewed change.

## Common changes

- New cap: add a `Config` field with a default, enforce it before exec when
  possible, add a fake-binary test that proves fail-closed behavior, and
  update the README bounds table.
- New tesseract or model version: update the apk pins in
  `scripts/ocr-sandbox/Dockerfile`, the digests in
  `scripts/prove-ocr-sandbox.sh`, and rerun the sandbox proof on every
  architecture.
- New fake behavior: add a mode to `runFakeTesseract` in `main_test.go`. The
  fake picks its mode from a `FAKEOCR:<mode>` token in the image bytes,
  because the engine scrubs the environment and fixes the argument vector.

## Anti-patterns

- Reading `ru_maxrss` for the child. On Linux it reports the Go parent's RSS.
- Treating `MaxAddressSpaceBytes` as the memory guarantee. The sandbox cgroup
  is the ceiling.
- Raising a cap past design 1737 section 6 without a design update.
- Asserting real-tesseract behavior in default unit tests. Use
  `sandbox_live_test.go`, which runs only inside the proof container.
