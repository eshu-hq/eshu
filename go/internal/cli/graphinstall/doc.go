// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package graphinstall verifies and installs a local NornicDB binary for
// `eshu install nornicdb` and `eshu graph upgrade`.
//
// Install resolves a source -- a local binary, a local archive/package, a
// download URL, or (when From is empty) the pinned release manifest for the
// running Eshu version -- verifies its checksum and reported version, and
// copies it into Eshu's managed home (<home>/bin/nornicdb-headless and
// <home>/graph-backends/nornicdb/manifest.json), writing an install
// manifest alongside it. An install that already matches the managed
// binary's version and checksum is reported as reused rather than
// rewritten, unless Options.Force is set.
//
// The package reads no cobra flags, and the process environment it does read
// is install-scoped rather than process wiring:
// ESHU_NORNICDB_INSTALL_TIMEOUT bounds an install-source download, and the
// managed home <home> above comes from eshulocal.ResolveHomeDir, which
// honours ESHU_HOME and otherwise falls back to the platform data directory
// (XDG_DATA_HOME on Linux, LOCALAPPDATA on Windows, ~/Library/Application
// Support on macOS). Every other piece of process state arrives as a
// parameter. go/cmd/eshu's
// graph_install_cmd.go is the thin cobra wrapper that resolves process
// state -- flags and the exit-code contract -- and calls into this package.
// This split is mechanical, not a design choice specific to this package:
// cmd/eshu is `package main`, so nothing can import it, and any symbol that
// reads a flag or maps to an exit code has to stay there.
//
// graphinstall also never executes a binary itself. Verifying that a
// candidate binary really is NornicDB requires running `<binary> version`,
// and that subprocess-execution logic belongs to the local_graph
// process-supervision cluster in go/cmd/eshu (readLocalGraphVersion in
// local_graph_process.go) -- a cluster docs/internal/design/package-restructure.md
// calls out as a real bidirectional cycle that has to move as one unit or
// not at all, so it is out of scope for this extraction. Callers thread
// their VersionReader implementation through Options.ReadVersion and
// ManagedBinaryIfPresent instead.
package graphinstall
