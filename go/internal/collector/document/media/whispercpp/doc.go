// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package whispercpp implements a local speech-to-text engine behind the
// media Engine contract (go/internal/collector/document/media) by running a
// pinned whisper.cpp whisper-cli binary out of process.
//
// New verifies the sha256 digests of the pinned binary and ggml model before
// it returns an Engine. Transcribe validates the WAV body in process
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
// no_speech.
//
// Results record the engine name, pinned version, model file name, and model
// digest. Redaction stays in media.Extract: this package returns raw segment
// text and never persists anything itself. Each run emits one span, the
// eshu_dp_media_transcript_engine_* metrics, and one log line, all without
// transcript text.
//
// The backend decision, sandbox profile, and image packaging status are
// recorded in README.md. No production caller is wired; enablement stays
// default-off and QA-tier only.
package whispercpp
