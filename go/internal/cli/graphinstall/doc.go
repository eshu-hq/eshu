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
// The package reads no cobra flags. The host state it does touch is
// install-scoped rather than process wiring, and this is the complete list.
//
// Two env call sites. ESHU_NORNICDB_INSTALL_TIMEOUT (source.go) bounds an
// install-source download. install.go passes both os.Getenv and
// os.UserHomeDir into eshulocal.ResolveHomeDir, so the managed home <home>
// above resolves from ESHU_HOME when set -- used verbatim with ~ expanded,
// with no "eshu" segment appended, unlike every other form below -- and
// otherwise per platform: HOME alone on macOS (<home>/Library/Application
// Support/eshu), XDG_DATA_HOME then HOME on Linux, LOCALAPPDATA then
// USERPROFILE on Windows. HOME and USERPROFILE count because os.UserHomeDir
// is defined as reading them.
//
// Three stdlib boundaries also consult the environment, which is why the
// list above is "what this package reads deliberately", not "all of it":
// the download's http.Client leaves Transport nil, so http.DefaultTransport
// honours HTTP_PROXY/HTTPS_PROXY/NO_PROXY; os.MkdirTemp and os.CreateTemp
// read TMPDIR; exec.Command resolves through PATH.
//
// go/cmd/eshu's graph_install_cmd.go is the thin cobra wrapper that resolves
// flags and the exit-code contract and calls into this package. That split is
// mechanical: cmd/eshu is `package main`, so nothing can import it, and any
// symbol reading a flag or mapping to an exit code has to stay there.
//
// graphinstall never executes the candidate NornicDB binary. Verifying that a
// candidate really is NornicDB requires running `<binary> version`, and that
// subprocess-execution logic belongs to the local_graph process-supervision
// cluster in go/cmd/eshu (readLocalGraphVersion in local_graph_process.go) --
// a cluster docs/internal/design/package-restructure.md calls out as a real
// bidirectional cycle that has to move as one unit or not at all, so it is
// out of scope for this extraction. Callers thread their VersionReader
// implementation through Options.ReadVersion and ManagedBinaryIfPresent
// instead. The package does run one other subprocess:
// exec.Command("pkgutil", "--expand-full", ...) in source.go, to expand a
// darwin .pkg install source. That is archive extraction, not version
// verification.
package graphinstall
