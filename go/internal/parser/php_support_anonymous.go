package parser

import (
	"fmt"
	"strings"
)

func phpAnonymousClassName(lineNumber int) string {
	return fmt.Sprintf("anonymous_class_%d", lineNumber)
}

func parsePHPAnonymousClass(trimmed string) (string, bool) {
	index := strings.Index(trimmed, "new class")
	if index < 0 {
		return "", false
	}
	remaining := strings.TrimSpace(trimmed[index+len("new class"):])
	return remaining, true
}
