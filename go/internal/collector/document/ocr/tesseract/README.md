# Tesseract OCR Engine

## Purpose

`collector/document/ocr/tesseract` is the general-purpose OCR backend for
image documentation (#9728, gate 2 of #8879). It reads mixed-case,
proportional-font text from PNG, JPEG, first-frame GIF, and WebP by running a
pinned tesseract binary out of process, one bounded subprocess per image.
The backend decision and its alternatives are recorded in
`docs/internal/design/9728-ocr-engine-backend.md`.

## Ownership boundary

This package owns the subprocess boundary: input checks before exec, digest
pinning, the bounded run, TSV parsing into `ocr.Region` values, and OCR
telemetry. It does not own redaction or fact building; those stay in
`ocr.Extract`, which `Engine.Extract` calls unchanged. It does not choose the
sandbox, discover images, add runtime flags, or wire a caller. The in-tree
5x7 `ocr.LocalEngine` stays as the deterministic fixture backend and
fail-closed fallback.

## Exported surface

See `doc.go` for the godoc contract.

- `Config` pins and bounds one installation: `BinaryPath`, `BinarySHA256`,
  `TessdataDir`, `ModelSHA256`, `EngineVersion` (required), plus caps and
  optional `Instruments`, `Tracer`, and `Logger`.
- `New` validates the config and verifies both digests.
- `Engine.Recognize` implements `ocr.Engine`.
- `Engine.Extract` runs `ocr.Extract` with this engine and records
  extraction telemetry.
- `Engine.EngineVersion` returns the recorded version string.
- `Outcome`, `Error`, and `OutcomeOf` classify failures.
- `EngineName` and the `Default*` cap constants.

## Bounds

Defaults follow the Image/OCR column of design 1737 section 6. Callers may
lower them.

| Field | Default | Enforced |
|---|---|---|
| `MaxInputBytes` | 25 MiB | before exec |
| `MaxPixels` | 50,000,000 | before exec, from the image header |
| `Timeout` | 30 s | context deadline; kills the process group |
| `MaxOutputBytes` | 8 MiB | stdout; overflow kills the run |
| `MaxStderrBytes` | 64 KiB | stderr is counted and discarded, never logged |
| `MaxRegions` | 500 | fails instead of truncating |
| `MaxAddressSpaceBytes` | 2 GiB | RLIMIT_AS via prlimit just after exec (Linux) |

The child runs `tesseract stdin stdout --oem 1 --psm 3 -l eng
--tessdata-dir <dir> tsv` with the environment set to `OMP_THREAD_LIMIT=1`,
`LC_ALL=C`, and `HOME`/`TMPDIR` pointing at its private scratch directory.

## Determinism

Each image gets a fresh process, so no adaptive state carries over. The LSTM
decoder is tesseract's fixed-width beam search with fixed weights and no
sampling. `OMP_THREAD_LIMIT=1` keeps it single threaded. The sandbox proof ran
each fixture 5 times per architecture with identical output. Output bytes are
not identical across architectures: arm64 and amd64 (qemu) differ in output
hash while the recognized clean line and redaction results match. See the
design record for details.

## Telemetry

| Signal | Name |
|---|---|
| Counter | `eshu_dp_ocr_recognitions_total` (`outcome`) |
| Histogram | `eshu_dp_ocr_recognition_duration_seconds` (`outcome`) |
| Histogram | `eshu_dp_ocr_engine_peak_rss_bytes` (sampled child VmHWM) |
| Counter | `eshu_dp_ocr_extractions_total` (`status`) |
| Counter | `eshu_dp_ocr_redacted_regions_total` |
| Spans | `ocr.extract`, `ocr.recognize` |
| Logs | `ocr recognition finished` (debug), `ocr recognition failed` (warn), `ocr extraction finished` |

Peak RSS comes from `/proc/<pid>/status` VmHWM, not rusage. Go spawns
children with `CLONE_VFORK|CLONE_VM`, and execve folds the parent's RSS
high-water mark into the child's `ru_maxrss`; a 32 MiB child under a 512 MiB
parent reported 590 MB that way (`TestRunReportsChildOnlyPeakRSS`).

## Verification

```sh
cd go && go test -race ./internal/collector/document/ocr/tesseract -count=1
bash scripts/prove-ocr-sandbox.sh                          # host architecture
bash scripts/prove-ocr-sandbox.sh --platform linux/amd64   # qemu on arm64 hosts
```

The unit tests drive the engine with the test binary acting as a fake
tesseract (`TestMain`). The sandbox script runs the real pinned binary under
`--network none --read-only --cap-drop ALL --user 65532:65532 --memory 512m
--pids-limit 64` against rendered fixtures with PII canaries.

## Gotchas / invariants

- Never put subprocess stdout or stderr into an error, log, or span.
- Keep regions line-grouped; word regions split a secret from its label and
  defeat redaction.
- Image preflight in `preflight/picture` still reports WebP as an
  unsupported codec, so `Engine.Extract` skips WebP even though
  `Engine.Recognize` reads it. Enabling WebP extraction is a separate
  reviewed change to `picture`.
- `MaxAddressSpaceBytes` is best effort: the limit lands just after exec and
  binfmt emulation applies it to qemu. The sandbox cgroup is the real
  memory ceiling.
- Binary digests are per architecture; `scripts/prove-ocr-sandbox.sh` pins
  arm64 and amd64 for `tesseract-ocr=5.5.0-r0`.

## Related docs

- `docs/internal/design/9728-ocr-engine-backend.md`
- `docs/internal/design/1737-visual-media-documentation-ingestion.md`
- `go/internal/collector/document/ocr/README.md`
