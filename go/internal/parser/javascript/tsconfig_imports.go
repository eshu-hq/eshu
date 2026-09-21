// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"strings"
)

func annotateJavaScriptResolvedImport(item map[string]any, resolver TSConfigImportResolver) {
	if item == nil {
		return
	}
	source, _ := item["source"].(string)
	if resolved := resolver.ResolveSource(strings.TrimSpace(source)); resolved != "" {
		item["resolved_source"] = resolved
	}
}
