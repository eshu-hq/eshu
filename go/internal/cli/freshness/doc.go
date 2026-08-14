// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package freshness holds the logic behind the three `eshu freshness`
// subcommands: generations, changed-since, and service-changed-since. It
// builds each request path from a selector set, reads the canonical Eshu
// response envelope through an EnvelopeFetcher the caller supplies, and writes
// either that envelope as JSON or a human summary to an io.Writer.
//
// RunGenerations, RunChangedSince, and RunServiceChangedSince are the whole
// command bodies. Each returns nil on success, a *Failure when the command
// must exit non-zero, or a plain write error. Callers that want the pieces
// separately can use the FetchX, RenderXSummary, and RenderEnvelopeError
// functions the Run functions are built from.
//
// Two behaviours are worth knowing before changing anything here.
//
// ExitCodeForErrorCode maps error *codes* only. A non-fresh freshness state --
// "building" or "stale" under truth.freshness.state -- is reported in the
// output and still exits 0, because reporting a still-building index is what
// these commands are for. That is a real difference from `eshu trace service`,
// `eshu change impact`, and `eshu map`, which all exit 4 on the same state.
//
// ErrorCodeFromTransport checks an error's message for "connection refused"
// and "request failed" before it looks at any HTTP status, so a response
// carrying both classifies as backend_unavailable. Reordering those checks
// changes operator-visible exit codes.
//
// The package reads no cobra flags, no process environment, and no command
// line, and it never calls os.Exit or touches os.Stdout. go/cmd/eshu's
// freshness.go, freshness_changed_since.go, and
// freshness_service_changed_since.go are the thin cobra wrappers that resolve
// that process state and convert a *Failure into the CLI's exit-code error
// type. The split is mechanical: go/cmd/eshu is package main, so nothing can
// import it, and any symbol that reads a flag or names commandExitError has to
// stay there.
package freshness
