// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package assistantguidance owns the logic behind `eshu assistant install`,
// `eshu assistant status`, and `eshu assistant uninstall`: maintaining a
// marker-delimited block of Eshu guidance inside the project instruction files
// that Claude Code, Codex/AGENTS.md-aware harnesses, and Cursor read.
//
// # The managed block
//
// Guidance lives between the BeginMarker and EndMarker HTML comments. That
// marker pair is the block's only identity. Upsert replaces exactly the bytes
// from the begin marker through the end marker and leaves every other byte in
// the file untouched; when no pair is present it appends a fresh block after
// the existing content, separated by one blank line. Remove strips the pair and
// its contents, collapsing the blank lines that bracketed it, and leaves the
// text above and below byte-identical. A file carrying a begin marker with no
// following end marker is treated as having no block, so a truncated file gains
// a fresh block instead of being spliced. Markers and their separating newlines
// are always LF; a CRLF file keeps its carriage returns in the surrounding
// content untouched, which is preservation, not CRLF support.
//
// # What this package writes
//
// Engine performs three file operations, all under the absolute root the caller
// supplies, and only at the platform-relative paths SupportedPlatforms lists
// (CLAUDE.md, AGENTS.md, and .cursor/rules/eshu.mdc):
//
//   - Install calls MkdirAll(dir, 0o755) then WriteFile(path, 0o644) -- and
//     only when the rendered content differs from what is already on disk, so
//     an unchanged reinstall performs no write at all. The write replaces the
//     file in place: it is not atomic, takes no backup, and 0o644 applies only
//     at creation because os.WriteFile leaves an existing file's mode alone.
//   - Uninstall calls WriteFile(path, 0o644) when a block was removed and other
//     content remains.
//   - Uninstall calls Remove(path) only when stripping the block leaves nothing
//     but whitespace, meaning the file held the Eshu block and nothing else. A
//     file that still has operator content is never deleted.
//
// Status writes nothing.
//
// # What reaches the block
//
// The body written inside the markers comes from GuidanceBody, which is
// assembled from package constants and depends only on the Platform. No
// operator-supplied value -- not the --path root, not the --platform filter,
// not the surrounding file's own content -- reaches the bytes between the
// markers. The operator's existing content is preserved verbatim outside the
// block, and the rendering functions print paths relative to the project root
// rather than the absolute root itself. Error values are the exception and
// carry the absolute path, which is what an operator needs to fix a failed
// write.
//
// # Ownership boundary
//
// This package reads no cobra flag, resolves no output stream, reads no process
// environment or working directory, and never calls os.Exit. RenderInstall,
// RenderStatus, and RenderUninstall write only to the io.Writer the caller
// supplies. go/cmd/eshu's assistant_guidance.go is the thin cobra wrapper: it
// declares the commands and flags, resolves --path against the process working
// directory, passes the resulting absolute root and platform list in as plain
// values, hands cmd.OutOrStdout() to the renderers, and maps returned errors to
// the CLI's exit-code contract. That split is mechanical, not a preference:
// go/cmd/eshu is package main, so nothing can import it, and any symbol that
// reads a flag or maps to an exit code has to stay there.
package assistantguidance
