// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import "strings"

// parseRuntimeTXT reads the single effective Heroku-style python-<version>
// line. The caller rejects multiple lines and invalid wrappers as a whole.
func parseRuntimeTXT(body string, sink *declarationSink) {
	eachLine(body, func(number int, line string) bool {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			return true
		}
		sink.add(declaration{line: number, runtime: "python", rawSpec: line})
		return true
	})
}

func extractRuntimeTXT(body string) Extraction {
	declarations, total := parse(sourceKindRuntimeTXT, body, 2)
	if total == 0 {
		return Extraction{}
	}
	if total != 1 || !strings.HasPrefix(declarations[0].rawSpec, "python-") || len(declarations[0].rawSpec) <= len("python-") {
		return Extraction{skip: Skip{Reason: SkipReasonManifestUnparseable, ByteSize: len(body)}}
	}
	if !safeSpec(declarations[0].rawSpec) {
		return Extraction{skip: Skip{Reason: SkipReasonValueRejected, ByteSize: len(body), Rejected: 1}}
	}
	return Extraction{sourceKind: sourceKindRuntimeTXT, declarations: declarations}
}
