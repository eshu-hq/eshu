# whisper.cpp Media Transcript Engine

## Purpose

`whispercpp` is the local speech-to-text backend behind the
`document/media` `Engine` contract (#9729, gate 2 of #8879). It runs a
whisper.cpp `whisper-cli` binary, built from a pinned source tag and
digest-verified at construction, out of process with a sha256-pinned ggml
model, and returns timed transcript segments. `media.Extract` then redacts and emits
facts through its existing path. The DTMF decoder in `../dtmf` stays as the
deterministic fixture backend.

## Ownership boundary

This package owns engine execution only: pin verification, WAV validation
and normalization, the bounded subprocess, output parsing, failure
classification, and per-run telemetry. It does not redact, persist, wire
callers, add runtime flags, or enable hosted extraction. No production
caller is wired. Use stays QA-tier only (the QA environment) until the caller lane and
security review land.

## Exported surface

See `doc.go` for the godoc contract.

- `New`, `Config`, `Limits`, `Engine` (`Transcribe` implements
  `media.Engine`), and `EngineName`.
- `Error`, `ErrorClass`, the `Class*` constants, `ClassOf`, and
  `ErrDigestMismatch`.

## Backend decision record

Requirement (arbiter ruling 2026-11-01): a local, pinned-model
speech-to-text engine, executed out of process under the gate-2 sandbox
profile, with greedy deterministic decoding, a pinned model sha256 recorded
with the engine name and version, and redaction left to `media.Extract`
unchanged. Cloud or hosted transcription is excluded by design 1737
("Non-Goals").

| Option | Verdict | Reason |
| --- | --- | --- |
| whisper.cpp `whisper-cli` out of process | Chosen | MIT. A crash, hang, or memory blow-up stays in a killable child with its own caps. No cgo in the Go build. Fixed flags give greedy decoding. JSON output on stdout. |
| whisper.cpp through a cgo binding | Rejected | Runs C++ inside the ingester process: a fault or runaway allocation takes the collector down, and per-run memory and time caps cannot be enforced in process. Adds cgo to the module build. |
| faster-whisper (CTranslate2) | Rejected | Needs a Python runtime, and there is no Python on the normal platform path. |
| Vosk / Kaldi | Rejected | Needs a library binding or a JVM/Python wrapper for a stable CLI. An accuracy comparison against Whisper was NOT_CHECKED. |
| Hosted speech API | Excluded | Design 1737 forbids cloud transcription. |
| DTMF decoder (`../dtmf`) | Kept as fixture backend | Deterministic and dependency-free, but it decodes keypad tones only. It does not discharge gate 2. |

### Pins

| Item | Pin |
| --- | --- |
| whisper.cpp source | `v1.9.5`, tag commit `d1be6fde11ac6e0407606b4e42fe72d34add8037`, MIT. Source-pinned: the build checks out the tag and fails unless HEAD equals that commit. |
| whisper-cli binary | Built from that source; the binary digest is not reproducible across toolchains. The operator passes the digest of the binary it built, and `New` verifies it. The proof script hashes the binary it extracts from its own image. |
| Model | `ggml-base.en.bin`, Hugging Face `ggerganov/whisper.cpp` revision `5359861c739e955e79d9a303bcbc70fb988958b1`, 147964211 bytes, sha256 `a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002`, model card license MIT |
| Build | static ggml/whisper libraries, `GGML_NATIVE=OFF`, `GGML_OPENMP=OFF`, no ffmpeg, SDL, or curl |

`New` refuses to build an `Engine` unless the built binary matches the
digest the caller supplies and the model matches its fixed sha256 pin
(`ErrDigestMismatch`). The proof Dockerfile also pins its frontend and base
image by digest and every apk package by exact version. Section facts record
`transcript_engine=whisper.cpp`, `transcript_engine_version`,
`transcript_model`, and `transcript_model_digest`.

### Upstream facts checked (v1.9.5 source)

Checked against `examples/cli/cli.cpp`, `examples/common-whisper.cpp`, and
`src/whisper.cpp` at the pinned tag:

- `-f -` reads audio from stdin into memory. `-of -` with `-ojf` writes full
  JSON to stdout. With stdout output the per-segment print callback is off,
  and `-np` disables the remaining prints.
- The CLI default beam size is 5, so greedy decoding needs `-bs 1`. `-bo 1`,
  `-tp 0`, and `-nf` (temperature increment 0) remove sampling and fallback.
  The decoder RNG is `std::mt19937(0)` and is used only above temperature 0.
- JSON `offsets` are milliseconds. String values escape only quotes and
  backslashes, so the parser blanks raw control bytes before decoding.
- The audio reader consults `WHISPER_COMMON_MINIAUDIO_SKIP`. The engine runs
  with an empty environment.

NOT_CHECKED: bit-identical output across CPU architectures (proof ran on
arm64 only); determinism across thread counts other than the fixed 4; Word
Error Rate (WER) on real recorded speech; a security review of the
miniaudio and ggml code that parses the canonical WAV and model.

### Formats

v1 accepts WAV only (PCM 8/16/24/32-bit, IEEE float 32-bit, and their
extensible forms), matching `preflight/media`, which already marks every
other design 1737 format as `unsupported_codec` before an engine is called.
The engine classifies a non-WAV `Media.Format` as `unsupported_format`.

`.mp3 .m4a .ogg .mp4 .mov .webm .mkv` stay unsupported. A pinned `ffmpeg`
subprocess with fixed arguments is the likely follow-up. It needs its own
dependency and decoder security review, because container demuxers are the
widest hostile-input surface in this path. whisper.cpp's built-in miniaudio
can decode MP3, FLAC, and Vorbis, but this package never hands caller bytes
to it.

### Normalization

`parseWAV` walks chunks with bounds checks. `normalizeWAV` then downmixes to
mono and resamples to 16 kHz, 16-bit, with a Blackman-windowed sinc filter
whose taps are fixed-point integers. A 10 kHz tone at 22.05 kHz is
suppressed; plain linear interpolation let it through at 0.589 of input RMS.
16 kHz input is copied without resampling. The engine therefore parses only
a WAV this package wrote.

### Limits and design ceilings

Defaults sit at or below the design 1737 section 6 audio/video ceilings, and
`New` rejects any configured value above them. Raising one needs a design
update and security review.

| Limit | Default | Ceiling (section 6) |
| --- | ---: | ---: |
| `MaxInputBytes` | 64 MiB | 250 MiB |
| `MaxDurationMillis` | 10 minutes | 30 minutes |
| `Timeout` | 10 minutes | 10 minutes |
| `MaxSegments` | 1,000 | 1,000 chunks |
| `MaxSegmentChars` | 4,096 runes | 16 KiB per chunk (4,096 runes at 4 bytes) |
| `MaxStdoutBytes` | 16 MiB | 512 MiB scratch |
| `MaxStderrBytes` | 64 KiB | none in section 6 |
| `MaxAddressSpaceBytes` | off | none in section 6; the container memory cap applies |

## Execution and bounds

### Required runtime sandbox (caller-applied)

The engine does not sandbox itself. A caller MUST run the engine, meaning
the Go process that calls `Transcribe` and therefore its `whisper-cli`
children, inside a container or namespace with this profile:

- no network (`--network none`); design 1737 section 6 requires external
  network access disabled for transcript extraction;
- a read-only root filesystem (`--read-only`), with the binary and model on
  it, so the files `New` hashed cannot change afterwards;
- all capabilities dropped (`--cap-drop ALL`) and
  `--security-opt no-new-privileges:true`;
- a non-root user (the proof uses `65532:65532`);
- memory and PID caps (the proof uses `--memory 2g --memory-swap 2g
  --pids-limit 64 --cpus 4`).

This is the gate-2 profile the arbiter ruling of 2026-11-01 requires and
that `scripts/verify-media-transcript-sandbox.sh` proves. `runProcess` only
adds an empty environment, `Dir=/`, its own process group, and the optional
`RLIMIT_AS`. Wiring the engine into a networked or writable process meets
every in-package check and still violates the design, so the caller lane
must prove this profile in its own deployment.

### In-process bounds

`runProcess` uses `exec.CommandContext` with the fixed argument vector, an
empty environment, `Dir=/`, stdin audio, and its own process group. It
creates no temporary files. Cancellation, the wall-clock `Timeout`, and any
stdout or stderr cap breach kill the whole process group. `WaitDelay` bounds
pipe draining. Input bytes and header duration are checked before decode
and exec. Segment count and length are checked after parsing.
`MaxAddressSpaceBytes` sets `RLIMIT_AS` through `prlimit` right after
`Start` (Linux only). The engine runs uncapped between exec and that call,
and the length of that window was not measured. The container memory limit
stays the primary memory cap. Other Unix systems reject a configured
address-space cap. Non-Unix platforms are rejected because they lack
process-group kill.

Segments that are only an annotation (`[BLANK_AUDIO]`, `(silence)`,
`*music*`, `♪`) are dropped and counted. A run with no remaining segments
returns `ClassNoSpeech`, wrapping `media.ErrNoSpeech`. Confidence is the
mean token probability over non-special tokens. Segment IDs are opaque
ordinals. Speaker labels and mention hints are never emitted.

## Dependencies

- `internal/collector/document/media` for the `Engine` contract and
  `ErrNoSpeech`.
- `internal/telemetry` for instruments, span name, and log helpers.
- `golang.org/x/sys/unix` for `prlimit` on Linux. No new module dependency.
- At runtime: the pinned `whisper-cli` and model files. They are not shipped
  in any Eshu image yet (see "Packaging").

## Telemetry

Each `Transcribe` call emits:

- Span `media.transcript_engine.run` with engine, version, model digest,
  format, outcome, input bytes, audio milliseconds, stdout/stderr bytes,
  engine peak RSS, and kept and dropped segment counts.
- `eshu_dp_media_transcript_engine_runs_total{outcome}` and
  `eshu_dp_media_transcript_engine_duration_seconds{outcome}`, where outcome
  is `completed` or an `ErrorClass` value.
- `eshu_dp_media_transcript_engine_segments_total{kind}`, where kind is
  `kept` or `non_speech_dropped`.
- One log line, `event_name=media.transcript_engine.run`, with
  `pipeline_phase=parsing` and the same numbers. It logs at WARN with
  `failure_class` on failure.

No signal carries transcript text, stderr, paths, or source names. Redaction
counts belong to `media.Extract` output (section `redacted` metadata); the
caller lane must count them when it wires extraction.

Collector Performance Evidence: baseline is no speech engine in tree (the
DTMF decoder returns zero segments on speech), so there is no prior
before-number for this path. After: `scripts/verify-media-transcript-sandbox.sh`
on 2026-11-01, arm64, whisper.cpp v1.9.5 with `ggml-base.en.bin`, 4 threads,
`--cpus 4 --memory 2g`. Input shape: one 5.551 s, 22.05 kHz mono 16-bit
espeak-ng WAV (244864 bytes). Each of 3 runs took 1.94-2.01 s wall time,
returned 2 segments, wrote 5487 stdout bytes, and peaked at about 246 MB
engine RSS. 6 s of silence took 1.78 s and ended `no_speech`. Normalization
is one pass over the input with a bounded tap table (at most 1024 phases),
and every run is capped by input bytes, duration, wall time, and output
bytes. The engine adds no worker, queue, graph write, or storage work. It is
safe because nothing calls it yet and each run fails closed at a cap.

Observability Evidence: each run emits the `media.transcript_engine.run`
span, `eshu_dp_media_transcript_engine_runs_total`,
`eshu_dp_media_transcript_engine_duration_seconds`,
`eshu_dp_media_transcript_engine_segments_total`, and one log line.
`telemetry_test.go` asserts outcome labels, segment kinds, span attributes,
and the absence of transcript text for completed, no_speech,
engine_failed, and timeout runs. The sandbox log lines above carried
`outcome`, `elapsed_seconds`, and `engine_peak_rss_bytes`, and the proof
script found no canary text in them.

## Sandbox proof

`scripts/verify-media-transcript-sandbox.sh` builds
`scripts/lib/media-transcript-sandbox/Dockerfile`, checks the model digest on
the host, and runs the `whispercpp_sandbox` tests in a container with
`--network none --read-only --cap-drop ALL --security-opt
no-new-privileges:true --user 65532:65532 --memory 2g --memory-swap 2g
--pids-limit 64 --cpus 4`. The fixture is synthesized at build time by
Alpine's `espeak-ng` from "Please restart the payments worker before noon.
The password is swordfish." Recorded result, 2026-11-01, arm64:

| Case | Outcome |
| --- | --- |
| Profile | uid 65532, root filesystem write denied, network unreachable |
| Speech, 3 runs | identical results, 2 segments each, 1.94-2.01 s |
| Redaction via `media.Extract` | 2 sections, 1 redacted, canary absent from facts and logs |
| 6 s digital silence | `no_speech` |
| 50 ms wall-clock cap | `timeout` after 0.087 s |
| 64-byte stdout cap | `resource_limit_exceeded` |
| 500 ms duration cap, hostile bytes | rejected before exec |
| 64 MiB address-space cap | `engine_failed` after 0.133 s |

base.en hears the synthetic phrase "payments worker" as "payment swearcare".
The same happens after sox's very-high-quality resampler, so this is a model
limit on synthetic speech. The proof asserts words that are stable across
resamplers.

## Packaging

The pinned binary and model are not in any runtime image yet. The proof
Dockerfile shows the build recipe. Adding them to the Eshu runtime image
would ship about 150 MB of model plus a C++ binary in every service image,
for a capability with no wired caller. It would also need the
`runtime-image-tools` gate (`scripts/verify-runtime-image-tools.sh`) to list
`whisper-cli` once a Go runtime execs it. That decision belongs with the
caller lane, which will likely prefer a dedicated transcription image or
the `eshu-ingester` satellite.

## Gotchas / invariants

- The stdout cap fires only once whisper.cpp writes output, which it does
  after decoding. The wall-clock cap is what bounds compute.
- `New` hashes the model, about 150 MB, once per construction. Build one
  `Engine` and reuse it.
- Pins are checked once, in `New`. Later runs exec the same paths without
  rehashing, so the read-only filesystem in the required runtime sandbox
  is what keeps the verified files from changing afterwards.

## Related docs

- `go/internal/collector/document/media/README.md`
- `docs/internal/design/1737-visual-media-documentation-ingestion.md`
- `docs/public/reference/telemetry/media-transcript-engine.md`
