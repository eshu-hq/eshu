// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package servicereport builds the offline service intelligence report for
// `eshu service-report`: reading a captured get_service_story (and optional
// supply-chain inventory) response, composing it through
// internal/serviceintel, and rendering the result for a terminal.
//
// The package reads no cobra flags, resolves no Eshu config or credential
// from the process environment, and never calls os.Exit. It reads two kinds
// of local input directly through the file-path parameters callers pass it
// -- the captured service-story response and the optional supply-chain
// inventory -- because a local file read behind an explicit path is
// mechanical input handling, not process wiring. go/cmd/eshu's
// service_report_cmd.go is the thin cobra wrapper that resolves flags
// (--from, --supply-chain-from, --json), reads process stdin, and maps
// errors to the CLI's exit-code contract; this package returns data and
// errors for the wrapper to print.
package servicereport
