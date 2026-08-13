// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package servicereport supplies the offline pieces of `eshu
// service-report`: reading a captured get_service_story (and optional
// supply-chain inventory) response, decoding it into the inputs
// internal/serviceintel composes from, and rendering the composed report
// for a terminal. Composition itself stays in the caller --
// serviceintel.FromServiceStory and serviceintel.Compose are called by the
// cobra wrapper, not by this package. The one serviceintel adapter this
// package calls is FromSupplyChainInventory, for the optional supply-chain
// file.
//
// The package reads no cobra flags, resolves no Eshu config or credential
// from the process environment, and never calls os.Exit. Its only operating
// system interaction is reading two kinds of local input through the
// file-path parameters callers pass it -- the captured service-story
// response and the optional supply-chain inventory -- because a local file
// read behind an explicit path is mechanical input handling, not process
// wiring. It writes no files, opens no network connections, and executes no
// subprocesses. go/cmd/eshu's service_report_cmd.go is the thin cobra
// wrapper that resolves flags (--from, --supply-chain-from, --json), passes
// process stdin in as an io.Reader, composes the report, and returns errors
// to main's exit-code handling; this package returns data and errors, and
// writes text only to the io.Writer the wrapper hands RenderReport.
package servicereport
