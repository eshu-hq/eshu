// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import (
	"bufio"
	"strings"
)

func instructionsFromSource(source string, escape rune) []instruction {
	scanner := bufio.NewScanner(strings.NewReader(source))
	instructions := make([]instruction, 0)
	escapeText := string(escape)
	var (
		buffer      strings.Builder
		startLine   int
		line        int
		heredocTerm string
		heredocRows []instruction
	)

	flush := func() {
		raw := strings.TrimSpace(buffer.String())
		buffer.Reset()
		if raw == "" || strings.HasPrefix(raw, "#") {
			return
		}
		parts := strings.Fields(raw)
		if len(parts) == 0 {
			return
		}
		keyword := strings.ToUpper(parts[0])
		value := strings.TrimSpace(strings.TrimPrefix(raw, parts[0]))
		if keyword == "RUN" && strings.HasPrefix(value, "<<") {
			marker := strings.Trim(strings.TrimPrefix(value, "<<"), "'\"")
			if marker != "" && !strings.ContainsAny(marker, " \t") {
				heredocTerm = marker
				heredocRows = nil
				return
			}
		}
		instructions = append(instructions, instruction{keyword: keyword, line: startLine, value: value})
	}

	for scanner.Scan() {
		line++
		text := scanner.Text()
		trimmed := strings.TrimSpace(text)
		if heredocTerm != "" {
			if trimmed == heredocTerm {
				instructions = append(instructions, heredocRows...)
				heredocTerm = ""
				heredocRows = nil
			} else if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				heredocRows = append(heredocRows, instruction{keyword: "RUN", line: line, value: trimmed})
			}
			continue
		}
		if trimmed == "" && buffer.Len() == 0 {
			continue
		}
		if strings.HasPrefix(trimmed, "#") && buffer.Len() == 0 {
			continue
		}
		if buffer.Len() == 0 {
			startLine = line
		} else {
			buffer.WriteByte(' ')
		}
		trimmedText := strings.TrimSpace(text)
		buffer.WriteString(strings.TrimSpace(strings.TrimSuffix(text, escapeText)))
		if strings.HasSuffix(trimmedText, escapeText) {
			continue
		}
		flush()
	}
	flush()
	if heredocTerm != "" {
		instructions = append(instructions, instruction{keyword: "RUN", line: line, value: "'"})
	}
	return instructions
}
