// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"log/slog"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// jsParseByteCap bounds the size of a single JavaScript/TypeScript/TSX file
// handed to tree-sitter. Normal hand-written source is tens of KB; the
// pathological tail is large generated bundles (minified webpack output,
// bundled vendor code) that tree-sitter parses superlinearly -- a 2.7MB
// webpack bundle measured 15.9s (~224x a normal parse) and a 3.4MB generated
// bundle measured 5.1s (#4766). 1 MiB is generous headroom above any
// hand-written file while remaining well below the pathological range.
const jsParseByteCap = 1 << 20

// ParserFactory returns a pooled tree-sitter parser for the requested runtime
// grammar name. The caller must return the parser via the paired ParserReturner
// after use instead of calling parser.Close directly.
type ParserFactory func(language string) (*tree_sitter.Parser, error)

// ParserReturner returns a borrowed parser to the runtime pool. It must be
// called with the same language name that was passed to ParserFactory.
type ParserReturner func(language string, p *tree_sitter.Parser)

// jsBoundedFileEvent records one file whose size exceeded jsParseByteCap and
// whose tree-sitter parse was skipped entirely.
type jsBoundedFileEvent struct {
	path          string
	originalBytes int
}

// row renders one bounded-file event as a payload row for
// payload["js_parse_bounded"].
func (e jsBoundedFileEvent) row() map[string]any {
	return map[string]any{
		"path":           e.path,
		"original_bytes": e.originalBytes,
		"action":         "file_skipped",
	}
}

// recordJSBoundedFile appends a js_parse_bounded payload row for one bounded
// file and emits a matching structured log line so a dropped parse is
// observable rather than silent.
func recordJSBoundedFile(payload map[string]any, path string, originalBytes int) {
	event := jsBoundedFileEvent{path: path, originalBytes: originalBytes}
	payload["js_parse_bounded"] = append(
		payload["js_parse_bounded"].([]map[string]any),
		event.row(),
	)
	slog.Warn(
		"javascript-family parse file bounded",
		"component", "parser.javascript",
		"path", event.path,
		"original_bytes", event.originalBytes,
		"action", "file_skipped",
	)
}
