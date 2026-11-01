# OCR Engine Backend Decision: Out-of-Process Tesseract LSTM (#9728)

Status: Accepted for the gate-2 lane of #8879 (2026-11-01). No caller is wired.

Related: #8879 (ingester extraction), #9382 (gate evidence), PR #9737 (the
5x7 `LocalEngine`), design 1737 (visual media ingestion).

## 1. Context

Gate 2 of #8879 needs a real OCR engine running bounded, with redaction,
under the sandbox profile proven on 2026-10-28. The merged `LocalEngine`
(PR #9737) matches 5x7 templates for `A-Z 0-9` and a few symbols. It has no
lowercase, no proportional fonts, and no WebP. The arbiter ruling of
2026-11-01 says it does not discharge gate 2 and requires:

> a LOCAL, pinned-model, general-purpose OCR engine (Tesseract-LSTM or an
> ONNX text recogniser) covering mixed-case proportional fonts and the
> design-1737 format list incl. WebP, executed OUT-OF-PROCESS under the
> sandbox profile proven 2026-10-28 (--network none, --read-only, --cap-drop
> ALL, non-root, memory/time/input caps), greedy deterministic decoding,
> model/binary digest pinned and recorded with engine name+version in
> section metadata (design 1737 §7.3), redaction via the existing Extract
> path UNCHANGED. External/hosted services are NOT acceptable.

`LocalEngine` stays as the deterministic fixture backend and fail-closed
fallback.

## 2. Options

| Option | Out of process | Deterministic | Sandbox fit | Cost |
|---|---|---|---|---|
| A. `tesseract` CLI, LSTM (`--oem 1`), one process per image | yes | yes: fixed weights, no sampling, single thread | stdin/stdout only; no writes needed | one distro package plus a 23 MB model |
| B. libtesseract through cgo (for example gosseract) | no, runs in the Go process | yes | fails the out-of-process rule; a crash or OOM takes the host down | static musl link of tesseract, leptonica, and libstdc++ through the `xx` cross build |
| C. ONNX text detector plus recogniser (for example PaddleOCR models on onnxruntime) | possible through a helper binary | yes; CTC greedy argmax | needs a custom helper binary | two models, a runtime, and our own detection post-processing; no stock CLI |
| D. Hosted OCR or vision API | n/a | n/a | excluded by design 1737 | rejected |
| E. Extend `LocalEngine` | n/a | yes | n/a | rejected by the ruling: not general purpose |

Option C's accuracy, footprint, and packaging were not measured
(NOT_CHECKED). It stays the fallback if strict greedy argmax becomes a hard
requirement.

## 3. Decision

Option A: run the distro `tesseract` 5.5.0 binary out of process from the
new `go/internal/collector/document/ocr/tesseract` package.

- Invocation: `tesseract stdin stdout --oem 1 --psm 3 -l eng --tessdata-dir
  <dir> tsv`. The man page documents `stdin`/`stdout` as the input and
  output names, `--oem 1` as "Neural nets LSTM only", `--psm 3` as fully
  automatic page segmentation, and the `tsv` config
  (`doc/tesseract.1.asc` upstream).
- Environment: only `OMP_THREAD_LIMIT=1`, `LC_ALL=C`, and `HOME`/`TMPDIR`
  set to a private 0700 scratch directory. The FAQ documents
  `OMP_THREAD_LIMIT=1` as the way to disable multithreading.
- Pins: `tesseract-ocr=5.5.0-r0`, `tesseract-ocr-data-eng=5.5.0-r0`,
  `leptonica=1.84.1-r0`, and `libwebp=1.4.0-r0` on Alpine 3.21 (digest
  pinned). Measured digests:
  - eng.traineddata `sha256:daa0c97d651c19fba3b25e81317cd697e9908c8208090c94c3905381c23fc047`
  - tesseract arm64 `sha256:62a760cd1c23d95211e7c42dddc761c2c9cb9488628941e9be6cd9b6748e9c77`
  - tesseract amd64 `sha256:38b441bd41257037a03d6a15d9f849d8bd83da4c24dab1df428a7a4ad265332f`
- Provenance: `New` hashes both files and rejects a mismatch. Each call
  re-checks file identity, size, and mtime. The recorded `EngineVersion` is
  `5.5.0; binary=sha256:<hex>; eng.traineddata=sha256:<hex>`. `ocr.Extract`
  copies it unchanged into section metadata `ocr_engine_version`, beside
  `ocr_engine=tesseract`. `Extract` needed no code change.
- Regions: TSV words grouped into lines. Redaction in `ocr.Extract` judges
  each region as one unit, so a `password: <value>` line must stay one
  region. A mutation to word-level regions fails the redaction test.

### 3.1 Determinism and the "greedy" wording

The tesseract LSTM recogniser decodes with `RecodeBeamSearch`, a fixed-width
beam search (`src/lstm/recodebeam.h`). It is not greedy argmax. The header
describes greedy "simple decoding" only as the baseline that beam search
improves on, and no CLI parameter selects it. The header mentions no
sampling or randomness.

The coordinator ruled on 2026-11-01:

> The arbiter ruling names "Tesseract-LSTM" as an acceptable class, and beam
> search is inherent to it, so the binding requirement is determinism (no
> sampling, fixed weights, one fresh process per image), not strict greedy
> argmax.

This design meets that requirement. A fresh process per image also avoids
the adaptive-classifier carry-over the FAQ warns about for reused API
objects.

Measured: 5 runs per fixture per architecture gave identical results. The
recognized text is identical across PNG, JPEG, GIF, and WebP on arm64 and on
amd64 under qemu (`text_sha256` `08bf5e2a…`). The full result, including
confidence and bounds, differs between the two architectures (PNG
`5214e31e…` on arm64, `892b04e3…` on amd64). Treat confidence and bounds as
architecture-specific. Byte identity on native amd64 hardware (the EKS
target) is NOT_CHECKED; qemu may not use the same SIMD path.

## 4. Bounds

Pre-exec checks: context, pinned-file identity, 25 MiB input, closed
PNG/JPEG/GIF/WebP magic, header dimensions (stdlib `DecodeConfig`, plus a
bounded RIFF parser for WebP VP8/VP8L/VP8X), and a 50 MP pixel cap. During
the run: a 30 s deadline that kills the process group, an 8 MiB stdout cap,
a 64 KiB stderr cap (counted, never logged), and a 500-region cap. Any other
outcome fails closed with no regions.

Memory: `RLIMIT_AS` (2 GiB default) is set with `prlimit` just after exec,
because `os/exec` has no pre-exec rlimit hook. The sandbox cgroup
(`--memory`) is the enforcing ceiling.

Peak RSS is sampled from `/proc/<pid>/status` VmHWM. `ru_maxrss` is wrong
for this: Go spawns with `CLONE_VFORK|CLONE_VM`, and execve folds the shared
parent mm's high-water mark into the child. A scratch probe showed the child
reporting 13, 310, and 823 MB for parents of 10, 300, and 800 MiB, while the
child's own VmHWM was 1.8 MB. The first race-detector run then failed every
real recognition against a 1 GiB RSS cap. `TestRunReportsChildOnlyPeakRSS`
is the regression for this.

## 5. Sandbox proof

`scripts/prove-ocr-sandbox.sh` builds the static engine test binary, bakes it
with the pinned binary, the pinned model, and rendered fixtures
(`scripts/ocr-sandbox/Dockerfile`), verifies both digests, and runs:

```text
docker run --network none --read-only --cap-drop ALL
  --security-opt no-new-privileges --user 65532:65532
  --memory 512m --memory-swap 512m --pids-limit 64 --cpus 1
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=16m
```

The fixtures render three DejaVu Sans lines: `Deploy the Checkout Service to
staging`, `password: Hunter2CanarySecret`, and `Contact
alice.canary@example.com today`. Results on 2026-11-01:

| Check | arm64 (native) | amd64 (qemu) |
|---|---|---|
| Clean mixed-case line exact, all 4 formats | pass | pass |
| 5 runs identical, all 4 formats | pass | pass |
| PNG/JPEG/GIF Extract: 3 sections, 2 redacted, no canary in envelopes | pass | pass |
| WebP Extract | skipped by preflight | skipped by preflight |
| Timeout 5 ms | `timeout` | `timeout` |
| RLIMIT_AS 48 MiB | `signaled` | not asserted (cap lands on qemu) |
| stdout cap 64 B | `output_oversize` | `output_oversize` |
| Pixel cap 1000 | `input_rejected` | `input_rejected` |
| Corrupt PNG body | `exec_failed` | `exec_failed` |
| SVG text with canary | `input_rejected` | `input_rejected` |
| Peak RSS (child) | ~80 MB | ~91 MB |
| Wall time per run (900x200) | ~0.21 s | ~6.6 s (emulated) |

A digest mismatch fails the script before the run. Seen on the first amd64
run against a placeholder pin.

## 6. Residuals

- WebP: the engine reads WebP, but `preflight/picture` still classifies it
  as `unsupported_codec`, so `ocr.Extract` skips it before the engine runs.
  Admitting WebP is a reviewed change to `picture` (its AGENTS.md requires a
  decoder dependency review). It is not in this lane.
- Runtime image packaging: the product image does not ship tesseract. Adding
  it needs the apk version floors (`scripts/verify-apk-floors.sh`),
  `REQUIRED_TOOLS` in `scripts/verify-runtime-image-tools.sh`, a Trivy pass on
  the new packages, and the reproducibility lane. That belongs with the
  caller lane, ideally in the extracted ingester image rather than the
  shared runtime image.
- Native amd64 byte identity, and RLIMIT_AS enforcement on amd64, are
  NOT_CHECKED.
- Redaction is only as good as recognition: a misread marker word (for
  example `passw0rd`) would not match `ocr.Extract`'s marker list. The
  fixtures read cleanly. Noisy-scan accuracy is NOT_CHECKED.
