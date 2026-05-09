package kotlin

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

var kotlinCastReceiverCallPattern = regexp.MustCompile(
	`\(([^()]+?)\s+as\??\s+([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*\??)\)\.([A-Za-z_]\w*)\s*\(`,
)

func kotlinAppendCastReceiverCalls(
	payload map[string]any,
	trimmed string,
	lineNumber int,
	functionDeclCutoff int,
	seenLineCalls map[string]struct{},
) {
	for _, match := range kotlinCastReceiverCallPattern.FindAllStringSubmatchIndex(trimmed, -1) {
		if len(match) != 8 {
			continue
		}
		if functionDeclCutoff >= 0 && match[0] < functionDeclCutoff {
			continue
		}

		fullName := strings.TrimSpace(trimmed[match[0]:match[7]])
		name := strings.TrimSpace(trimmed[match[6]:match[7]])
		if fullName == "" || name == "" {
			continue
		}

		callKey := fullName + "#" + strconv.Itoa(lineNumber)
		if _, ok := seenLineCalls[callKey]; ok {
			continue
		}
		seenLineCalls[callKey] = struct{}{}

		shared.AppendBucket(payload, "function_calls", map[string]any{
			"name":              name,
			"full_name":         fullName,
			"inferred_obj_type": strings.TrimSuffix(strings.TrimSpace(trimmed[match[4]:match[5]]), "?"),
			"line_number":       lineNumber,
			"lang":              "kotlin",
		})
	}
}
