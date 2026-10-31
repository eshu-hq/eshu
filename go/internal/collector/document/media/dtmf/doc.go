// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package dtmf implements a local deterministic transcript engine behind the
// media Engine contract (go/internal/collector/document/media).
//
// A dtmf Engine decodes Dual-Tone Multi-Frequency keypad tones from WAV PCM
// audio with Goertzel frequency analysis and returns one timestamped segment
// per isolated tone group. Only the tones genuinely present in the signal
// become segment text; toneless or speech-like audio yields zero segments, and
// the package never invents lexical content.
//
// Backend decision (#9729): a native speech-to-text library binding was
// rejected because it needs cgo, large model-weight artifacts, and a
// transcription-dependency review, and its output is not bit-stable across
// model versions. An external transcription service was rejected because the
// media design forbids cloud transcription, network fetches, and credential
// handling in this path. The local deterministic decoder below needs no new
// module dependency, performs no network or filesystem I/O, allocates only
// bounded frame buffers, honors context cancellation, and returns
// bit-identical output for identical input, so it is the only backend that
// satisfies the sandbox, determinism, and redaction constraints without a
// separate dependency review. Full speech transcription remains out of scope
// until that reviewed design lands.
//
// Bounded execution: Options caps input bytes, header-derived duration,
// emitted segment count, and per-segment text length. Caps are checked before
// decoding where possible; breaches fail loudly instead of truncating facts.
//
// Redaction before facts: segment IDs are opaque ordinals, speaker labels and
// mention hints are never emitted, and digit runs of two or more consecutive
// digits are withheld entirely (no segment, no text) because multi-digit
// keypad entry is treated as potentially sensitive account, PIN, or number
// content. Withheld runs produce no section facts downstream.
//
// This package owns the engine only. Caller wiring, runtime enablement,
// telemetry, and hosted extraction paths stay caller-owned; extraction remains
// default-off, use stays QA-tier only, and no production caller is claimed.
package dtmf
