package parser

import (
	"regexp"
	"strings"
)

var kotlinReceiverPreservingScopeFunctionPattern = regexp.MustCompile(
	`(?s)\.(?:also|apply)\s*\{.*?\}`,
)

func kotlinStripReceiverPreservingScopeFunctions(expression string) string {
	expression = strings.TrimSpace(expression)
	for {
		next := kotlinReceiverPreservingScopeFunctionPattern.ReplaceAllString(expression, "")
		if next == expression {
			return expression
		}
		expression = strings.TrimSpace(next)
	}
}
