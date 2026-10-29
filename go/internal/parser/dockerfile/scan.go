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
	var buffer strings.Builder
	var pending *instruction
	var body []string
	var terms []heredocMarker
	startLine, line := 0, 0

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
		item := instruction{keyword: keyword, line: startLine, value: value}
		if keyword == "RUN" || keyword == "COPY" || keyword == "ADD" {
			markers := heredocMarkers(value)
			if len(markers) > 0 {
				item.heredoc = true
				item.heredocExecutable = keyword == "RUN" && simpleExecutableHeredoc(value, markers)
				pending = &item
				terms = markers
				body = body[:0]
				return
			}
		}
		instructions = append(instructions, item)
	}

	for scanner.Scan() {
		line++
		text := scanner.Text()
		trimmed := strings.TrimSpace(text)
		if pending != nil {
			if terms[0].matches(text) {
				terms = terms[1:]
				if len(terms) == 0 {
					pending.heredocComplete = true
					pending.heredocBody = strings.Join(body, "\n")
					instructions = append(instructions, *pending)
					pending = nil
					body = nil
				}
			} else {
				body = append(body, text)
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
	if pending != nil {
		pending.heredocBody = strings.Join(body, "\n")
		instructions = append(instructions, *pending)
	} else {
		flush()
	}
	return instructions
}
