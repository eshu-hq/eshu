package dockerfile

import "strings"

const defaultEscapeRune = '\\'

type keyValueToken struct {
	name  string
	value string
}

func splitKeyValueTokens(raw string, escape rune) []keyValueToken {
	fields := splitDockerfileWords(raw, escape)
	result := make([]keyValueToken, 0, len(fields))
	for _, field := range fields {
		name, value, found := strings.Cut(field, "=")
		if !found {
			continue
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" {
			continue
		}
		result = append(result, keyValueToken{name: name, value: value})
	}
	return result
}

func dockerfileEscapeRune(source string) rune {
	for _, rawLine := range strings.Split(source, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			return defaultEscapeRune
		}
		if !strings.HasPrefix(line, "#") {
			return defaultEscapeRune
		}
		directive, value, found := strings.Cut(strings.TrimPrefix(line, "#"), "=")
		if !found {
			return defaultEscapeRune
		}
		if strings.EqualFold(strings.TrimSpace(directive), "escape") {
			for _, char := range strings.TrimSpace(value) {
				return char
			}
			return defaultEscapeRune
		}
	}
	return defaultEscapeRune
}

// splitDockerfileWords follows Dockerfile command-line style token rules for
// the metadata instructions this package models. Quotes group spaces and
// the Dockerfile escape directive escapes the following rune.
func splitDockerfileWords(raw string, escape rune) []string {
	if escape == 0 {
		escape = defaultEscapeRune
	}
	words := make([]string, 0)
	var current strings.Builder
	var quote rune
	escaped := false
	for _, char := range raw {
		if escaped {
			current.WriteRune(char)
			escaped = false
			continue
		}
		if char == escape {
			escaped = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
				continue
			}
			current.WriteRune(char)
			continue
		}
		if char == '"' || char == '\'' {
			quote = char
			continue
		}
		if char == ' ' || char == '\t' || char == '\n' || char == '\r' {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(char)
	}
	if escaped {
		current.WriteRune(escape)
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}
	return words
}
