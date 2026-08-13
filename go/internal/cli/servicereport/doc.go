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
// from the process environment, and never calls os.Exit. Its whole
// operating-system surface is three reads and one write, all of them behind
// a parameter the caller supplies: os.ReadFile on the --from path
// (ReadInput), os.ReadFile on the --supply-chain-from path
// (SupplyChainSection), io.ReadAll on the io.Reader it is handed when no
// --from path is given, and the io.Writer RenderReport formats into. A
// local file read behind an explicit path is mechanical input handling, not
// process wiring. Nothing here touches os.Stdin or os.Stdout by name, writes
// a file, opens a network connection, or executes a subprocess.
//
// go/cmd/eshu's service_report_cmd.go is the thin cobra wrapper that
// resolves flags (--from, --supply-chain-from, --json), passes process stdin
// in as that io.Reader, composes the report, and returns errors to main,
// which maps them to an exit code.
package servicereport
