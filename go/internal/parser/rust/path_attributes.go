package rust

import (
	"strconv"
	"strings"
)

func rustPathAttributeCandidate(attributes []string) string {
	for _, attribute := range attributes {
		if rustAttributePath(attribute) != "path" {
			continue
		}
		value := rustAttributeValue(attribute)
		if value != "" {
			return value
		}
	}
	return ""
}

func rustAttributeValue(attribute string) string {
	trimmed := strings.TrimSpace(attribute)
	trimmed = strings.TrimPrefix(trimmed, "#[")
	trimmed = strings.TrimSuffix(trimmed, "]")
	_, value, ok := strings.Cut(trimmed, "=")
	if !ok {
		return ""
	}
	value = strings.TrimSpace(value)
	if unquoted, err := strconv.Unquote(value); err == nil {
		return strings.TrimSpace(unquoted)
	}
	return strings.Trim(value, `"`)
}
