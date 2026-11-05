// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package main is the zero-fact synthetic emitter for the #8877 enabled
// window.
//
// The binary speaks the collector SDK satellite protocol over stdio: it reads
// one extensionhost.Request from stdin and writes one SDK result to stdout.
// Every invocation returns ResultUnchanged with status rows only and no
// facts, so the claim completes with zero fact, reducer, and graph deltas.
// It performs no source observation, holds no credentials, and makes no
// network calls; its only input is the host's request bytes.
//
// It is derived from templates/collector (the lawful standalone collector
// source, --sdk-stdio branch) with the observation core removed: the window
// proves the claim path carries an unchanged result, not that a source was
// read. The host validates the echoed claim field-for-field, so the emitter
// returns the incoming claim verbatim.
package main
