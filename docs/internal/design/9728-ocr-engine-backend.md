# OCR Engine Backend Decision: Out-of-Process Tesseract LSTM (#9728)

Status: Accepted for the gate-2 lane of #8879 (2026-11-01). No caller is wired,
and gate 2 is not met (section 6).

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
  <dir> -c tessedit_create_tsv=1 -c tessedit_create_txt=0`. The man page
  (`doc/tesseract.1.asc` upstream) documents `stdin`/`stdout` as the input
  and output names, `--oem 1` as "Neural nets LSTM only", `--psm 3` as fully
  automatic page segmentation, and `-c CONFIGVAR=VALUE`. The `-c` pair
  replaces the `tsv` config name, which would read the unpinned file
  `tessdata/configs/tsv` (its whole content is `tessedit_create_tsv 1`).
  Both forms gave byte-identical output on the proof fixture
  (`sha256:241d3fcd…`).
- Page segmentation: only `--psm` modes that need no OSD model are accepted
  (3-11 and 13). Mode 1 tried to open `osd.traineddata`, which is not
  pinned; modes 0, 1, 2, and 12 are refused.
- Environment: only `OMP_THREAD_LIMIT=1`, `LC_ALL=C`, and `HOME`/`TMPDIR`
  set to a private 0700 scratch directory. The FAQ documents
  `OMP_THREAD_LIMIT=1` as the way to disable multithreading.
- Provenance of record: the digest-pinned Alpine 3.21 base plus exact apk
  versions `tesseract-ocr=5.5.0-r0`, `tesseract-ocr-data-eng=5.5.0-r0`,
  `leptonica=1.84.1-r0`, and `libwebp=1.4.0-r0` in
  `scripts/ocr-sandbox/Dockerfile`. Image-level pinning is the real
  provenance; the per-file hashes below are the runtime tamper check. The
  arbiter's confirmation that image-level pinning is acceptable is
  requested (see section 6).
- Runtime tamper check: `New` hashes, and each call re-stats (identity,
  size, mtime):
  - the 67 KB `tesseract` front end;
  - every shared object `ldd` lists for it in the image, 15 files including
    `libtesseract.so.5.0.5`, `libleptonica.so.6.0.0`, the codec libraries,
    `libgomp`, and the musl loader;
  - `eng.traineddata`.
  The library set digest is SHA-256 over sorted `<path> <sha256>` lines.
  `New` also runs `tesseract --version` once and fails unless the first line
  is `tesseract <EngineVersion>`, so the release string is checked rather
  than only configured.
- Measured pins (per architecture except the model):
  - eng.traineddata `sha256:daa0c97d651c19fba3b25e81317cd697e9908c8208090c94c3905381c23fc047`
  - tesseract arm64 `sha256:62a760cd…`; amd64 `sha256:38b441bd…`
  - library set arm64 `sha256:5556b1aa…`; amd64 `sha256:7a62cadd…`
  The full values live in `scripts/prove-ocr-sandbox.sh`.
- Recorded version: `5.5.0; binary=sha256:<hex>; libs=sha256:<hex> (15
  files); eng.traineddata=sha256:<hex>`. `ocr.Extract` copies it unchanged
  into section metadata `ocr_engine_version`, beside `ocr_engine=tesseract`.
  `Extract` needed no code change.
- Not adopted: exec through the verified file descriptor (fexecve). The
  loader still maps the libraries by path, and an in-place write to the held
  inode would defeat it too. The sandbox's read-only root filesystem is what
  prevents in-place modification.
- Regions: TSV words grouped into lines. Redaction in `ocr.Extract` judges
  each region as one unit, so a `password: <value>` line must stay one
  region. A mutation to word-level regions fails the redaction test. A label
  and value on separate lines are not covered (section 6).

### 3.1 Determinism and the "greedy" wording

The tesseract LSTM recogniser decodes with `RecodeBeamSearch`, a fixed-width
beam search (`src/lstm/recodebeam.h`). It is not greedy argmax. The header
describes greedy "simple decoding" only as the baseline that beam search
improves on, and no CLI parameter selects it. The header mentions no
sampling or randomness.

Arbiter-eshu ruling 2026-11-01 (F3-a, this PR): fixed-width beam search
meets the determinism intent of "greedy" for Tesseract-LSTM. The conditions
are digest-pinned weights, no sampling or RNG, fixed beam widths,
`OMP_THREAD_LIMIT=1`, and one fresh process per image. This design meets
each one. A fresh process per image also avoids the adaptive-classifier
carry-over the FAQ warns about for reused API objects.

Measured: 5 runs per fixture per architecture gave identical results. The
recognized text is identical across PNG, JPEG, GIF, and WebP on arm64 and on
amd64 under qemu (`text_sha256` `08bf5e2a…`). The full result, including
confidence and bounds, differs between the two architectures (PNG
`46796c01…` on arm64, `2647bb05…` on amd64). Treat confidence and bounds as
architecture-specific. Byte identity on native amd64 hardware (the EKS
target) is NOT_CHECKED; qemu may not use the same SIMD path.

## 4. Bounds

Pre-exec checks: context, pinned-file identity, 25 MiB input, closed
PNG/JPEG/GIF/WebP magic, header dimensions (stdlib `DecodeConfig`, plus a
bounded RIFF parser for WebP VP8/VP8L/VP8X), a 50 MP pixel cap, and the
128 MiB decoded-bytes cap of design 1737 section 6. Decoded size is
estimated from the header as pixels x 4 bytes, leptonica's 32 bpp RGB form
and the largest it holds for any supported input, so it binds first at about
33.5 MP. During the run: a 30 s deadline that kills the process group, an
8 MiB stdout cap, a 64 KiB stderr cap (counted, never logged), and a
500-region cap. Any other outcome fails closed with no regions.

Memory: `RLIMIT_AS` (2 GiB default) is set with `prlimit` just after exec,
because `os/exec` has no pre-exec rlimit hook. The sandbox cgroup
(`--memory`) is the enforcing ceiling. The engine does not bound
concurrency: N parallel calls run N processes, each allowed the full
`RLIMIT_AS`, so the caller must size its worker count against the sandbox
memory limit.

Peak RSS is sampled from `/proc/<pid>/status` VmHWM. `ru_maxrss` is wrong
for this: Go spawns with `CLONE_VFORK|CLONE_VM`, and execve folds the shared
parent mm's high-water mark into the child. In a scratch probe, a `/bin/sh`
child reported `ru_maxrss` of 13,232, 310,448, and 822,960 KiB under parents
holding 10, 300, and 800 MiB. Its own VmHWM was 1,828 kB each time. An
earlier draft enforced a 1 GiB cap on `ru_maxrss`. Under `-race`, the
parent's RSS pushed every fake-binary unit-test run past that cap
(`memory_exceeded`). `TestRunReportsChildOnlyPeakRSS` is the regression for
this.

## 5. Sandbox proof

`scripts/prove-ocr-sandbox.sh` builds the static engine test binary, bakes it
with the pinned binary, libraries, model, and rendered fixtures
(`scripts/ocr-sandbox/Dockerfile`), verifies every pin, and runs:

```text
docker run --network none --read-only --cap-drop ALL
  --security-opt no-new-privileges --user 65532:65532
  --memory 512m --memory-swap 512m --pids-limit 64 --cpus 1
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=16m
```

The fixtures render three DejaVu Sans lines: `Deploy the Checkout Service to
staging`, `password: Hunter2CanarySecret`, and `Contact
alice.canary@example.com today`. The proof test asserts each row below.
Results on 2026-11-01:

| Check | arm64 (native) | amd64 (qemu) |
|---|---|---|
| Clean mixed-case line exact, all 4 formats | pass | pass |
| 5 runs identical, all 4 formats | pass | pass |
| PNG/JPEG/GIF Extract: completed, 3 sections, 2 redacted, no canary in envelopes | pass | pass |
| WebP Extract | skipped by preflight | skipped by preflight |
| Timeout 5 ms | `timeout` | `timeout` |
| RLIMIT_AS 48 MiB | `signaled` | not asserted (cap lands on qemu) |
| stdout cap 64 B | `output_oversize` | `output_oversize` |
| Pixel cap 1000 | `input_rejected` | `input_rejected` |
| Corrupt PNG body | `exec_failed` | `exec_failed` |
| SVG text with canary | `input_rejected` | `input_rejected` |
| Peak RSS (child) | about 80 MB | about 92 MB |
| Wall time per run (900x200) | about 0.2 s | about 6.6 s (emulated) |

A pin mismatch fails the script before the run. Seen on the first amd64 run
and the first library-set run, both against placeholder pins.

## 6. Residuals

This PR does not meet gate 2 of #8879. The design-1737 format list is not
covered end to end, and there is no QA-tier caller.

- WebP: the engine reads WebP, but `preflight/picture` still classifies it
  as `unsupported_codec`, so `ocr.Extract` skips it before the engine runs.
  Admitting WebP is a reviewed change to `picture`, tracked in #9780.
- Label and value on separate lines: line grouping protects a secret only
  when tesseract puts its label on the same line. In a form, column, or
  label-above-value layout, the value can be its own region, and
  `ocr.Extract`'s markers do not match a bare value.
  `TestExtractLeavesValueOnlyLineUnredacted` pins today's behavior; the fix
  belongs to the caller lane in #9782.
- Pinning: the arbiter's confirmation is requested that image-level pinning
  (base image digest plus apk versions) is the provenance of record, with the
  per-file hashes as the runtime tamper check.
- Mixed architectures: binary and library digests differ per architecture,
  so `ocr_engine_version` differs between an arm64 and an amd64 worker for
  the same release. A caller running mixed nodes sees version churn in
  section metadata and must not treat it as a re-extraction trigger.
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
