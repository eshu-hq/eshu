// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import "log/slog"

// maxImportSourceBytes bounds the module specifier an import, require or
// re-export entry may carry into the imports bucket. The canonical projector
// turns that specifier into the name of a (:Module {name}) node, and Neo4j's
// range index on that property rejects oversized keys, failing the whole
// repository's atomic canonical write (issue #7056).
//
// The limit comes from measured data: across 542,380 import/require/re-export
// specifiers in 50 locally cloned repositories the longest legitimate one was
// 121 bytes (p99.99 = 119 bytes), and npm caps a package name at 214
// characters. 1024 bytes is more than 4x the npm name cap and 8x the longest
// observed specifier, so it only ever trips on extraction garbage.
const maxImportSourceBytes = 1024

// dropOversizedImportSources removes every imports-bucket entry whose source
// exceeds maxImportSourceBytes and logs each drop, so a specifier the parser
// should never have produced is visible to an operator instead of reaching the
// graph. This is defence in depth: the extraction itself only reads string
// literal specifiers from the grammar.
func dropOversizedImportSources(payload map[string]any, path string) {
	items, ok := payload["imports"].([]map[string]any)
	if !ok {
		return
	}
	kept := items[:0]
	for _, item := range items {
		source, _ := item["source"].(string)
		if len(source) <= maxImportSourceBytes {
			kept = append(kept, item)
			continue
		}
		importType, _ := item["import_type"].(string)
		lineNumber, _ := item["line_number"].(int)
		slog.Warn(
			"javascript-family import source exceeds bound",
			"component", "parser.javascript",
			"path", path,
			"import_type", importType,
			"line_number", lineNumber,
			"source_bytes", len(source),
			"max_source_bytes", maxImportSourceBytes,
			"action", "import_dropped",
		)
	}
	payload["imports"] = kept
}
