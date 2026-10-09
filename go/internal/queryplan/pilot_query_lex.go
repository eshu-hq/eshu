// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

func pilotTokens(source string) ([]string, error) {
	var tokens []string
	for i := 0; i < len(source); {
		if unicode.IsSpace(rune(source[i])) {
			i++
			continue
		}
		if strings.HasPrefix(source[i:], "--") || strings.HasPrefix(source[i:], "//") {
			for i < len(source) && source[i] != '\n' {
				i++
			}
			continue
		}
		if strings.HasPrefix(source[i:], "/*") {
			end := strings.Index(source[i+2:], "*/")
			if end < 0 {
				return nil, errors.New("unterminated block comment")
			}
			i += end + 4
			continue
		}
		if source[i] == '\'' || source[i] == '"' || source[i] == '`' {
			quote := source[i]
			i++
			closed := false
			for i < len(source) {
				if source[i] == quote {
					if i+1 < len(source) && source[i+1] == quote {
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				if source[i] == '\\' && i+1 < len(source) {
					i += 2
				} else {
					i++
				}
			}
			if !closed {
				return nil, errors.New("unterminated query literal")
			}
			tokens = append(tokens, "<literal>")
			continue
		}
		if source[i] == ';' {
			return nil, errors.New("multiple pilot statements are unsupported")
		}
		if pilotWordByte(source[i]) || source[i] == '$' {
			start := i
			i++
			for i < len(source) && (pilotWordByte(source[i]) || (source[start] == '$' && source[i] == '$')) {
				i++
			}
			tokens = append(tokens, strings.ToUpper(source[start:i]))
			continue
		}
		if i+1 < len(source) {
			pair := source[i : i+2]
			switch pair {
			case "::", "->", "<-", "<=", ">=", "!=", "<>", "..", "=>":
				tokens = append(tokens, pair)
				i += 2
				continue
			}
		}
		if strings.ContainsRune("()[]{}.,:*+-/=<>|%!?", rune(source[i])) {
			tokens = append(tokens, source[i:i+1])
			i++
			continue
		}
		return nil, fmt.Errorf("unsupported pilot query syntax byte %q", source[i])
	}
	return tokens, nil
}

func pilotWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '_'
}

func pilotValidName(value string) bool {
	if value == "" {
		return false
	}
	first := value[0]
	if (first < 'a' || first > 'z') && (first < 'A' || first > 'Z') && first != '_' {
		return false
	}
	for i := 1; i < len(value); i++ {
		if !pilotWordByte(value[i]) {
			return false
		}
	}
	return true
}

func pilotBalanced(tokens []string) error {
	var stack []string
	for _, tok := range tokens {
		switch tok {
		case "(", "[", "{":
			stack = append(stack, tok)
		case ")", "]", "}":
			if len(stack) == 0 {
				return errors.New("unbalanced pilot query")
			}
			opening := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if opening == "(" && tok != ")" || opening == "[" && tok != "]" || opening == "{" && tok != "}" {
				return errors.New("unbalanced pilot query")
			}
		}
	}
	if len(stack) > 0 {
		return errors.New("unbalanced pilot query")
	}
	return nil
}

func containsPilotToken(tokens []string, wanted string) bool {
	for _, token := range tokens {
		if token == wanted {
			return true
		}
	}
	return false
}

func pilotTraversalBounds(tokens []string) error {
	for i := 0; i < len(tokens); i++ {
		if tokens[i] != "[" {
			continue
		}
		end := i + 1
		for end < len(tokens) && tokens[end] != "]" {
			end++
		}
		for j := i + 1; j < end; j++ {
			if tokens[j] != "*" {
				continue
			}
			upper := ""
			if j+1 < end && pilotDigits(tokens[j+1]) {
				upper = tokens[j+1]
			}
			if j+1 < end && tokens[j+1] == ".." && j+2 < end {
				upper = tokens[j+2]
			}
			if j+2 < end && tokens[j+2] == ".." {
				upper = ""
				if j+3 < end {
					upper = tokens[j+3]
				}
			}
			if !pilotDigits(upper) {
				return errors.New("unbounded variable-length traversal")
			}
		}
		i = end
	}
	return nil
}

func pilotDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := range value {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func pilotSchemaPresent(name string, statements []string) bool {
	for _, statement := range statements {
		tokens, err := pilotTokens(statement)
		if err != nil || len(tokens) < 3 || tokens[0] != "CREATE" || (tokens[1] != "INDEX" && tokens[1] != "CONSTRAINT") {
			continue
		}
		if tokens[2] == strings.ToUpper(name) {
			return true
		}
	}
	return false
}

func pilotFinalOrderLimit(tokens []string, kind string) (int, int) {
	order, limit, depth := -1, -1, 0
	for i, token := range tokens {
		switch token {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
		if depth != 0 {
			continue
		}
		if token == "ORDER" && i+1 < len(tokens) && tokens[i+1] == "BY" {
			order = i
		}
		if token == "LIMIT" {
			limit = i
		}
	}
	_ = kind
	return order, limit
}
