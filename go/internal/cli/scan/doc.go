// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package scan runs the `eshu scan` family's work: index a local source with
// eshu-bootstrap-index, then prove the result is queryable rather than merely
// submitted.
//
// Execute is the entry point. It preflights the pipeline status and a bounded
// query, runs the bootstrap child, and — unless Options.Wait is false — polls
// until EvaluateReadiness reports a drained, healthy pipeline. Readiness here
// means the queue is empty, no stage or domain holds failed or dead-letter
// work, a generation completed or is active, and health reads healthy. Process
// health alone is never readiness, and a status report with no generation
// history is treated as not-ready rather than drained. Execute's Result is
// meaningful even alongside a non-nil error: it carries the last status report
// and the evidence gathered so far, which is what the CLI's JSON envelope
// reports on failure.
//
// This package holds no process state. Runtime carries every collaborator that
// touches the process — the API read surface, the base URL recorded as
// evidence, the environment the bootstrap child inherits, PATH lookup, and the
// child process itself — and Execute rejects a Runtime missing any of them
// instead of reaching a nil dereference part way through a scan. Runtime.Now
// and Runtime.Wait are pure clock helpers and default when nil.
// go/cmd/eshu/scan.go is the thin cobra wrapper that reads the flags, resolves
// the streams, wires those seams in defaultScanRuntime, and renders the
// envelope; that file is package main, so nothing here can call back into it.
//
// The environment this package reads is scan-scoped, not process wiring. There
// are two deliberate reads:
//
//  1. ESHU_GRAPH_BACKEND (CurrentGraphBackend) — the backend label on the truth
//     envelope, "unknown" when unset rather than a guessed default.
//  2. eshulocal.BuildLayout, to which ReposDir passes os.Getenv and
//     os.UserHomeDir, resolving the managed home: ESHU_HOME when set, with ~
//     expanded and no eshu segment appended; otherwise LOCALAPPDATA on Windows
//     and XDG_DATA_HOME elsewhere, each falling back to os.UserHomeDir, which
//     is defined as reading HOME and USERPROFILE.
//
// Two stdlib boundaries also read process state without a visible os.Getenv:
// filepath.Abs resolves a relative path against the working directory, and
// os.UserHomeDir is reached through the callback above.
//
// The package names two API routes, /api/v0/status/pipeline and
// /api/v0/repositories?limit=1, but never dials them: FetchPipelineStatus and
// FetchQueryProbe read them through the caller's Client. It imports no cobra,
// no net/http, and no os/exec, calls no os.Exit, and prints nothing to the
// process streams — every write goes to an io.Writer the caller passed in.
package scan
