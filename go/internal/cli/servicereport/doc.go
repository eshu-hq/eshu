// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package servicereport supplies the offline pieces of `eshu
// service-report`: reading a captured get_service_story (and optional
// supply-chain inventory) response, decoding it into the inputs
// internal/serviceintel composes from, and rendering the composed report
// for a terminal.
//
// This package never composes a report. Nothing in it calls
// serviceintel.Compose or serviceintel.FromServiceStory: the cobra wrapper
// calls both and hands the finished serviceintel.Report to RenderReport.
// Composition belongs to internal/serviceintel, and it is not the CLI's
// either -- internal/serviceintelhttp serves the same report over HTTP and
// internal/answerquality composes reports for its corpus, neither of them
// going anywhere near this package. The one serviceintel function this
// package's production code calls is FromSupplyChainInventory, for the
// optional supply-chain file. (The package's own tests do call Compose, so
// RenderReport is exercised against a genuinely composed report and not only
// a hand-built stand-in.)
//
// The package reads no cobra flags, resolves no Eshu config or credential
// from the process environment, and never calls os.Exit. Outside its tests,
// its whole operating-system surface is three reads and one write, all of
// them behind a parameter the caller supplies: os.ReadFile on the --from
// path (ReadInput), os.ReadFile on the --supply-chain-from path
// (SupplyChainSection), io.ReadAll on the io.Reader it is handed when no
// --from path is given, and the io.Writer RenderReport formats into. A
// local file read behind an explicit path is mechanical input handling, not
// process wiring. No production code here touches os.Stdin or os.Stdout by
// name, writes a file, opens a network connection, or executes a
// subprocess. The tests do write files, into t.TempDir, to build the
// captured-response fixtures they feed back in; that is fixture setup, not
// package surface.
//
// ReadInput returns an error in three cases: the file read failed, the stdin
// read failed, or stdin was read and turned out to be empty or all
// whitespace. That last check is there for the message, not to prevent an
// empty report -- empty bytes fail at decode either way; see ReadInput's own
// documentation. The path parameter is tested with strings.TrimSpace, so a
// whitespace-only --from is treated as no path at all and stdin is read
// instead -- the file is never opened. SupplyChainSection applies the same
// TrimSpace test before deciding whether to read its file.
//
// RenderReport prints a fixed subset of the composed report -- the subject
// label, the report-level supported/partial/truth_class, per-section
// status/title/summary/unsupported_reasons/limitations, the next-call labels
// and reasons, and the suggested investigations. Everything else the --json
// output carries is absent from the text by design, so the two modes are not
// meant to agree field for field. RenderReport's documentation lists the
// subset, and TestRenderReportJSONKeyContract pins it key by key.
//
// go/cmd/eshu's service.go is the thin cobra wrapper that
// resolves flags (--from, --supply-chain-from, --json), passes process stdin
// in as that io.Reader, composes the report, and returns errors to main,
// which maps them to an exit code.
package servicereport
