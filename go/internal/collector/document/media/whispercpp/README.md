# whisper.cpp Media Transcript Engine

## Purpose

`whispercpp` is the local speech-to-text backend behind the
`document/media` `Engine` contract (#9729, gate 2 of #8879). It runs a pinned
whisper.cpp `whisper-cli` binary out of process with a pinned ggml model and
returns timed transcript segments. `media.Extract` then redacts and emits
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
| whisper.cpp | `v1.9.5`, tag commit `d1be6fde11ac6e0407606b4e42fe72d34add8037`, MIT |
| Model | `ggml-base.en.bin`, Hugging Face `ggerganov/whisper.cpp` revision `5359861c739e955e79d9a303bcbc70fb988958b1`, 147964211 bytes, sha256 `a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002`, model card license MIT |
| Build | static ggml/whisper libraries, `GGML_NATIVE=OFF`, `GGML_OPENMP=OFF`, no ffmpeg, SDL, or curl |

`New` refuses to build an `Engine` unless both the binary and the model match
their sha256 pins (`ErrDigestMismatch`). Section facts record
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

## Execution and bounds

`runProcess` uses `exec.CommandContext` with the fixed argument vector, an
empty environment, `Dir=/`, stdin audio, and its own process group. It
creates no temporary files. Cancellation, the wall-clock `Timeout`, and any
stdout or stderr cap breach kill the whole process group. `WaitDelay` bounds
pipe draining. Input bytes and header duration are checked before decode
and exec. Segment count and length are checked after parsing.
`MaxAddressSpaceBytes` sets `RLIMIT_AS` through `prlimit` right after start
(Linux only), a few microseconds before model load. The container memory
limit stays the primary memory cap. Other Unix systems reject a configured
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

Collector Performance Evidence: in the sandbox proof the 5.55 s fixture
transcribes in 1.94-2.01 s wall time with about 246 MB engine peak RSS
(arm64, 4 threads, `--cpus 4`). WAV normalization is a single pass with a
bounded tap table (at most 1024 phases).

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
- Pins are checked at construction. Keep the binary and model on a
  read-only filesystem so they cannot change after verification.

## Related docs

- `go/internal/collector/document/media/README.md`
- `docs/internal/design/1737-visual-media-documentation-ingestion.md`
- `docs/public/reference/telemetry/media-transcript-engine.md`
