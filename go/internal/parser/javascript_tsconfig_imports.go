package parser

import (
	"strings"

	jsparser "github.com/eshu-hq/eshu/go/internal/parser/javascript"
)

func annotateJavaScriptResolvedImport(item map[string]any, resolver jsparser.TSConfigImportResolver) {
	if item == nil {
		return
	}
	source, _ := item["source"].(string)
	if resolved := resolver.ResolveSource(strings.TrimSpace(source)); resolved != "" {
		item["resolved_source"] = resolved
	}
}
