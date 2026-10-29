// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import (
	"strings"
	"unicode"
)

type heredocMarker struct {
	term      string
	stripTabs bool
}

func (marker heredocMarker) matches(line string) bool {
	if marker.stripTabs {
		line = strings.TrimLeft(line, "\t")
	}
	return line == marker.term
}

// heredocMarkers finds here-doc operators outside quoted words. A malformed
// marker uses an impossible delimiter so remaining source is not reinterpreted
// as independent Dockerfile instructions.
func heredocMarkers(value string) []heredocMarker {
	var markers []heredocMarker
	var quote byte
	escaped := false
	for index := 0; index+1 < len(value); index++ {
		char := value[index]
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			continue
		}
		if char != '<' || value[index+1] != '<' {
			continue
		}
		index += 2
		marker := heredocMarker{}
		if index < len(value) && value[index] == '-' {
			marker.stripTabs = true
			index++
		}
		if index < len(value) && (value[index] == '\'' || value[index] == '"') {
			delimiterQuote := value[index]
			index++
			start := index
			for index < len(value) && value[index] != delimiterQuote {
				index++
			}
			marker.term = value[start:index]
			if index < len(value) {
				index++
			}
		} else {
			start := index
			for index < len(value) && !unicode.IsSpace(rune(value[index])) && !strings.ContainsRune(";&|<>", rune(value[index])) {
				index++
			}
			marker.term = value[start:index]
		}
		if marker.term == "" {
			marker.term = "\x00unsupported-heredoc"
		}
		markers = append(markers, marker)
		index--
	}
	return markers
}

func simpleExecutableHeredoc(value string, markers []heredocMarker) bool {
	if len(markers) != 1 {
		return false
	}
	fields := strings.Fields(value)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "<<") {
		return false
	}
	if len(fields) == 1 {
		return true
	}
	if len(fields) != 2 {
		return false
	}
	switch fields[1] {
	case "sh", "bash", "ash", "/bin/sh", "/bin/bash", "/bin/ash":
		return true
	default:
		return false
	}
}

func parseInstallHeredoc(item instruction, stage string, variables *installScope) ([]Install, []UnparsedCommand, int) {
	if !item.heredocComplete {
		return nil, []UnparsedCommand{{LineNumber: item.line, Reason: "unterminated_heredoc"}}, 0
	}
	if !item.heredocExecutable || len(item.heredocBody) > maxInstallCommandBytes {
		return nil, []UnparsedCommand{{LineNumber: item.line, Reason: "unsupported_heredoc"}}, 0
	}
	var installs []Install
	dropped := 0
	for index, line := range strings.Split(item.heredocBody, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		segments, ok := shellSegments(line)
		if !ok || len(segments) == 0 {
			return nil, []UnparsedCommand{{LineNumber: item.line, Reason: "unsupported_heredoc_body"}}, dropped
		}
		for _, segment := range segments {
			if !simpleHeredocCommand(segment) {
				return nil, []UnparsedCommand{{LineNumber: item.line, Reason: "unsupported_heredoc_body"}}, dropped
			}
			if segment[0] == "set" || segment[0] == "echo" || segment[0] == "true" {
				continue
			}
			found, reason, count := parseInstallSegment(segment, item.line+index+1, stage, variables)
			dropped += count
			if reason != "" {
				return nil, []UnparsedCommand{{LineNumber: item.line, Reason: reason}}, dropped
			}
			installs = append(installs, found...)
		}
	}
	return installs, nil, dropped
}

func simpleHeredocCommand(words []string) bool {
	if len(words) == 0 {
		return false
	}
	command := strings.TrimPrefix(strings.TrimPrefix(words[0], "/usr/bin/"), "/bin/")
	switch command {
	case "apt", "apt-get", "apk", "yum", "dnf", "pecl", "npm", "pip", "pip3", "docker-php-ext-install", "docker-php-ext-enable", "docker-php-ext-configure", "a2enmod", "curl", "wget", "set", "echo", "true":
		return true
	default:
		return false
	}
}
