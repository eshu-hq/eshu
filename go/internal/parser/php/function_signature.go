package php

import (
	"regexp"
	"strings"
)

var phpParameterNamePattern = regexp.MustCompile(`\$(\w+)`)

func extractPHPParameters(lines []string, startIndex int, rawLine string) []string {
	signature := rawLine
	for index := startIndex; index < len(lines) && !strings.Contains(signature, ")"); index++ {
		if index == startIndex {
			continue
		}
		signature += " " + strings.TrimSpace(lines[index])
	}
	start := strings.Index(signature, "(")
	end := strings.LastIndex(signature, ")")
	if start < 0 || end < 0 || end <= start {
		return nil
	}
	rawParams := signature[start+1 : end]
	if strings.TrimSpace(rawParams) == "" {
		return []string{}
	}
	parts := splitPHPCommaSeparated(rawParams)
	parameters := make([]string, 0, len(parts))
	for _, part := range parts {
		match := phpParameterNamePattern.FindStringSubmatch(part)
		if len(match) != 2 {
			continue
		}
		parameters = append(parameters, "$"+match[1])
	}
	return parameters
}
