// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package servicereport supplies the offline pieces of `eshu
// service-report`: reading a captured get_service_story (and optional
// supply-chain inventory) response, decoding it into the inputs
// internal/serviceintel composes from, and rendering the composed report
// for a terminal. Composition itself stays in the caller -- on the CLI path
// serviceintel.FromServiceStory and serviceintel.Compose are called by the
// cobra wrapper, not by this package's production code. (report_test.go
// calls both, to render a real composed report instead of a hand-built
// stand-in.) The one serviceintel adapter this package's production code
// calls is FromSupplyChainInventory, for the optional supply-chain file.
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
// captured-response fixtures they feed back in.
//
// ReadInput returns an error in three cases: the file read failed, the stdin
// read failed, or stdin was read and turned out to be empty or all
// whitespace. The path parameter is tested with strings.TrimSpace, so a
// whitespace-only --from is treated as no path at all and stdin is read
// instead -- the file is never opened. SupplyChainSection applies the same
// TrimSpace test before deciding whether to read its file.
//
// go/cmd/eshu's service_report_cmd.go is the thin cobra wrapper that
// resolves flags (--from, --supply-chain-from, --json), passes process stdin
// in as that io.Reader, composes the report, and returns errors to main,
// which maps them to an exit code.
package servicereport
