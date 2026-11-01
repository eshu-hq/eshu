# Media Transcript Engine Telemetry

The local whisper.cpp speech-to-text engine
(`go/internal/collector/document/media/whispercpp`, #9729) emits one span,
three metrics, and one log line per transcription run. No caller is wired
yet, so these signals appear only once a QA-tier caller injects the engine.
No signal carries transcript text, engine stderr, file paths, or source
names.

| Signal | Labels or attributes | Use |
| --- | --- | --- |
| `eshu_dp_media_transcript_engine_runs_total` | `outcome` | Runs by closed outcome: `completed`, `no_speech`, `timeout`, `canceled`, `resource_limit_exceeded`, `unsupported_format`, `malformed_media`, `malformed_output`, or `engine_failed`. A rising `engine_failed` usually means a bad pin, a memory-cap kill, or a broken binary. |
| `eshu_dp_media_transcript_engine_duration_seconds` | `outcome` | Wall time from input validation through output parsing. Compare `completed` p95 with the configured timeout. |
| `eshu_dp_media_transcript_engine_segments_total` | `kind` | `kept` speech segments and `non_speech_dropped` annotations such as `[BLANK_AUDIO]`. |
| Span `media.transcript_engine.run` | engine, version, model digest, format, outcome, input bytes, audio millis, stdout/stderr bytes, engine peak RSS, segment counts | Per-run detail. The span status is an error for every failure outcome except `no_speech`. |
| Log `event_name=media.transcript_engine.run` | `pipeline_phase=parsing`, the same numbers, and `failure_class` on failure | INFO on success or no speech, WARN on failure. |

Redaction counts are not engine signals. `media.Extract` marks redacted
sections with `redacted=true` metadata, and the caller that wires extraction
owns counting them.
