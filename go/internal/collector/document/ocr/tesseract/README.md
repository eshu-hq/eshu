# Tesseract OCR Engine

## Purpose

`collector/document/ocr/tesseract` is the general-purpose OCR backend for
image documentation (#9728, toward gate 2 of #8879, which it does not meet
on its own; see the design record). It reads mixed-case,
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
  `Libraries`, `TessdataDir`, `ModelSHA256`, `EngineVersion` (required),
  plus caps and optional `Instruments`, `Tracer`, and `Logger`.
- `PinnedFile` is one runtime library path with its pinned digest.
- `New` validates the config, verifies every digest (binary, each library,
  model), and runs `tesseract --version` once to confirm `EngineVersion`.
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
| `MaxDecodedBytes` | 128 MiB | before exec: pixels x 4 bytes (leptonica 32 bpp), so about 33.5 MP binds first |
| `Timeout` | 30 s | context deadline; kills the process group |
| `MaxOutputBytes` | 8 MiB | stdout; overflow kills the run |
| `MaxStderrBytes` | 64 KiB | stderr is counted and discarded, never logged |
| `MaxRegions` | 500 | fails instead of truncating |
| `MaxAddressSpaceBytes` | 2 GiB | RLIMIT_AS via prlimit just after exec (Linux) |

The child runs `tesseract stdin stdout --oem 1 --psm 3 -l eng
--tessdata-dir <dir> -c tessedit_create_tsv=1 -c tessedit_create_txt=0`
with the environment set to `OMP_THREAD_LIMIT=1`, `LC_ALL=C`, and
`HOME`/`TMPDIR` pointing at its private scratch directory. The `-c` flags
replace the `tsv` config name, which would read the unpinned
`tessdata/configs/tsv`. Only `--psm` modes that need no OSD model are
accepted (3-11, 13).

The engine does not bound concurrency. N parallel calls run N processes,
each allowed the full `MaxAddressSpaceBytes`, so the caller must size its
worker count against the sandbox memory limit.

## Determinism

Each image gets a fresh process, so no adaptive state carries over. The LSTM
decoder is tesseract's fixed-width beam search with fixed weights and no
sampling. `OMP_THREAD_LIMIT=1` keeps it single threaded. Arbiter-eshu ruled
on 2026-11-01 (F3-a) that this meets the determinism intent of "greedy";
see the design record. The sandbox proof ran
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

## Accuracy eval

`scripts/eval-ocr-wer.sh` (#10073) scores word-level WER
(`WER = (S + D + I) / N`, case/punctuation/whitespace folded; CER as the
secondary metric) of the real binary over a deterministic degradation
ladder baked into `scripts/ocr-sandbox/Dockerfile`: the clean control plus
light (JPEG q60 + 1-degree skew), medium (+ seeded Gaussian noise +
downscale/upscale round-trip), and heavy (+ blur + contrast reduction) for
the basic, form-stacked, and key-body layouts, with sibling `.txt` ground
truths. `TestSandboxWEREval` emits `OCR_WER_EVIDENCE` JSON lines. The prose-layout
clean controls must score WER 0 (the key-body base64 ladder is report-only:
measured clean WER 0.47 on arm64, base64 glyphs misread at this size) and
every level must withhold the canary (a misread canary is still redacted,
or the whole image is withheld, or the run fails); degraded-level WER is
report-only until the band is ratified from 3 enforcement-host runs.
Measured rows land in `testdata/accuracy/ocr-media-wer-baseline.json` via
`scripts/refresh-ocr-media-wer-baseline.sh`, which updates measured fields
only — thresholds are separate reviewed edits.

## Gotchas / invariants

- A start failure is classified like a running one: a caller context that
  ended is `canceled` and a passed run deadline is `timeout`, even when
  `cmd.Start` returns before the process exists. Only a start failure with
  both contexts live (missing or non-executable binary) is `exec_failed`.
- Never put subprocess stdout or stderr into an error, log, or span.
- Keep regions line-grouped; word regions split a secret from its label and
  defeat redaction.
- Line grouping does not cover a label and value on separate lines (forms,
  columns, label above value). `ocr.Extract` handles that case, not this
  package: see "Label and value redaction" in the OCR README.
  `TestExtractRedactsValueOnLineBelowLabel` and the `form-stacked.png` and
  `form-columns.png` sandbox fixtures prove it through this engine.
- Image preflight in `preflight/picture` still reports WebP as an
  unsupported codec, so `Engine.Extract` skips WebP even though
  `Engine.Recognize` reads it. Enabling WebP extraction is #9780.
- `MaxAddressSpaceBytes` is best effort: the limit lands just after exec and
  binfmt emulation applies it to qemu. The sandbox cgroup is the real
  memory ceiling.
- Provenance of record: the digest-pinned Alpine 3.21 index
  (`alpine:3.21@sha256:48b0309c…`) plus the exact, Alpine-signed apk version
  of every package that owns an object `ldd` lists for `/usr/bin/tesseract`
  (15 objects, 15 packages, same versions on arm64 and amd64) in
  `scripts/ocr-sandbox/Dockerfile`. The per-architecture digests in
  `scripts/prove-ocr-sandbox.sh` (binary, library set, eng.traineddata) are
  the pin the proof enforces after every build and the engine re-checks at
  construction and per call. Gate-2 criterion 'model digest pinned in the
  image' is met for eng.traineddata (`sha256:daa0c97d…`); arbiter-eshu
  ruling 2026-11-01 (pinning).
- Because those digests are per architecture, `ocr_engine_version` differs
  between arm64 and amd64 workers for the same release. A caller on mixed
  nodes must not treat that difference as a reason to re-extract.
- The engine execs by path, not through the verified descriptor: the loader
  maps libraries by path anyway, and the read-only root filesystem is what
  prevents in-place modification. The loader opens each `.so.N` symlink,
  while the `Config` paths are the `readlink -f` targets, so a re-pointed
  symlink is not caught by the stat check; the read-only root is the defense
  there too. `ldd` lists only DT_NEEDED objects, and `libgomp.so.1` can
  `dlopen` offload plugins, but none is installed, the environment is
  scrubbed (`runner.go`), and the root is read-only, so no plugin load path
  exists.

## Related docs

- `docs/internal/design/9728-ocr-engine-backend.md`
- #1737
- `go/internal/collector/document/ocr/README.md`
