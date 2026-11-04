// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package whispercpp implements a local speech-to-text engine behind the
// media Engine contract (go/internal/collector/document/media) by running a
// whisper.cpp whisper-cli binary, built from a pinned source tag, out of
// process.
//
// New verifies the sha256 digest of the built binary and the pinned ggml
// model before it returns an Engine. Transcribe validates the WAV body in process
// (format, layout, byte cap, header-derived duration cap), normalizes it to
// 16 kHz mono 16-bit PCM with integer arithmetic, and pipes that canonical
// WAV to the engine on stdin. The engine writes full JSON on stdout. There is
// no shell, no temporary file, and an empty environment. Arguments are fixed
// at construction: CPU only, greedy decoding (beam size 1, best-of 1),
// temperature 0, no temperature fallback, a fixed language, and a fixed
// thread count.
//
// Every run is bounded: a wall-clock timeout, stdout and stderr byte caps
// that kill the process group on breach, caller cancellation, segment-count
// and segment-length caps, and an optional Linux RLIMIT_AS cap. Failures
// return a classified *Error whose message never carries transcript text,
// stderr, paths, or source names. A run that hears only non-speech
// annotations, or nothing, returns a ClassNoSpeech error wrapping
// media.ErrNoSpeech, which media.Extract records as transcript_status
// no_speech. An annotation that holds a sensitive keyword, such as
// "(The password is)", may be speech a model wrapped in brackets: the engine
// keeps it and leaves the redaction to media.Extract, so a run with only that
// annotation returns one segment, not ClassNoSpeech.
//
// Results record the engine name, pinned version, model file name, and model
// digest. Redaction stays in media.Extract: Transcribe returns raw segment
// text and never persists anything itself. Each run emits one span, the
// eshu_dp_media_transcript_engine_* metrics, and one log line, all without
// transcript text. Engine.Extract runs media.Extract with this engine and
// adds caller-counted extraction telemetry: one media.transcript_extract
// span, eshu_dp_media_transcript_extractions_total by closed status, and
// eshu_dp_media_transcript_redacted_segments_total, all without transcript
// text.
//
// The engine does not sandbox itself. A caller must run it in a container
// or namespace with no network, a read-only root filesystem holding the
// binary and model, all capabilities dropped, no-new-privileges, a non-root
// user, and memory and PID caps, as design 1737 section 6 and the gate-2
// arbiter ruling require. New rejects limits above the section 6 ceilings.
//
// The backend decision, sandbox profile, and image packaging status are
// recorded in README.md. The only wired caller is
// go/cmd/collector-media-import, a disabled-by-default one-shot drain with
// explicit QA-tier-only injection; no production caller beyond the QA tier is
// claimed.
package whispercpp
