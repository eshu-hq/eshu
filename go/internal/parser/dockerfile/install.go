// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import (
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

const maxInstallCommandBytes = 8192

// Install records one package, extension, Apache module, or downloaded artifact
// declared by a Dockerfile instruction. It is source evidence, not a claim
// that the resulting image contains the named software.
type Install struct {
	Manager    string
	Name       string
	Version    string
	LineNumber int
	Stage      string
}

// UnparsedCommand reports a RUN or ADD instruction whose install content was
// not safely understood. The command body is deliberately omitted.
type UnparsedCommand struct {
	LineNumber int
	Reason     string
}

func installMap(item Install) map[string]any {
	row := map[string]any{"manager": item.Manager, "name": item.Name, "line_number": item.LineNumber, "stage": item.Stage}
	addOptional(row, "version", item.Version)
	return row
}

func installRows(installs []Install) []map[string]any {
	rows := make([]map[string]any, 0, len(installs))
	for index, install := range installs {
		row := installMap(install)
		row["install_ordinal"] = strconv.Itoa(index + 1)
		row["category"] = installCategory(install.Manager)
		rows = append(rows, row)
	}
	return rows
}

func installCategory(manager string) string {
	switch manager {
	case "url":
		return "download"
	case "docker-php-ext-install", "docker-php-ext-enable", "docker-php-ext-configure", "pecl":
		return "php_extension"
	case "a2enmod":
		return "apache_module"
	default:
		return "package"
	}
}

func unparsedMap(item UnparsedCommand) map[string]any {
	return map[string]any{"line_number": item.LineNumber, "reason": item.Reason}
}

func parseInstallInstruction(item instruction, stage string, variables map[string]string) ([]Install, []UnparsedCommand, int) {
	if len(item.value) > maxInstallCommandBytes {
		return nil, []UnparsedCommand{{LineNumber: item.line, Reason: "command_over_limit"}}, 0
	}
	if item.keyword == "ADD" {
		words, ok := shellWords(item.value)
		if !ok || len(words) < 2 {
			return nil, []UnparsedCommand{{LineNumber: item.line, Reason: "invalid_add"}}, 0
		}
		source := words[0]
		if strings.HasPrefix(source, "--") && len(words) > 2 {
			source = words[1]
		}
		if !strings.Contains(source, "://") {
			return nil, nil, 0
		}
		name, dropped, ok := safeDownload(source)
		if dropped {
			return nil, nil, 1
		}
		if !ok {
			return nil, []UnparsedCommand{{LineNumber: item.line, Reason: "invalid_url"}}, 0
		}
		return []Install{{Manager: "url", Name: name, LineNumber: item.line, Stage: stage}}, nil, 0
	}
	segments, ok := shellSegments(item.value)
	if !ok {
		return nil, []UnparsedCommand{{LineNumber: item.line, Reason: "invalid_shell_words"}}, 0
	}
	installs := make([]Install, 0)
	unparsed := make([]UnparsedCommand, 0)
	dropped := 0
	for _, segment := range segments {
		found, reason, count := parseInstallSegment(segment, item.line, stage, variables)
		installs = append(installs, found...)
		dropped += count
		if reason != "" {
			unparsed = append(unparsed, UnparsedCommand{LineNumber: item.line, Reason: reason})
		}
	}
	return installs, unparsed, dropped
}

func parseInstallSegment(words []string, line int, stage string, vars map[string]string) ([]Install, string, int) {
	if len(words) == 0 {
		return nil, "", 0
	}
	command := strings.TrimPrefix(words[0], "/usr/bin/")
	command = strings.TrimPrefix(command, "/bin/")
	manager, start := "", 0
	switch command {
	case "apt", "apt-get", "apk", "yum", "dnf", "pecl", "npm", "pip", "pip3":
		if len(words) < 2 {
			return nil, "missing_action", 0
		}
		action := words[1]
		switch {
		case (command == "apt" || command == "apt-get" || command == "yum" || command == "dnf" || command == "pip" || command == "pip3" || command == "pecl") && action == "install":
		case command == "apk" && action == "add":
		case command == "npm" && action == "install":
		default:
			return nil, "", 0
		}
		manager, start = command, 2
	case "docker-php-ext-install", "docker-php-ext-enable", "docker-php-ext-configure", "a2enmod":
		manager, start = command, 1
	case "curl", "wget":
		for _, word := range words[1:] {
			if !strings.Contains(word, "://") {
				continue
			}
			expanded, ok := expandInstallWord(word, vars)
			if !ok {
				return nil, "unresolved_variable", 0
			}
			name, dropped, valid := safeDownload(expanded)
			if dropped {
				return nil, "", 1
			}
			if !valid {
				return nil, "invalid_url", 0
			}
			return []Install{{Manager: "url", Name: name, LineNumber: line, Stage: stage}}, "", 0
		}
		return nil, "missing_url", 0
	default:
		return nil, "", 0
	}
	if manager == "npm" {
		global := false
		for _, word := range words[start:] {
			if word == "-g" || word == "--global" {
				global = true
			}
		}
		if !global {
			return nil, "", 0
		}
	}
	rows := make([]Install, 0)
	for _, word := range words[start:] {
		if strings.HasPrefix(word, "-") {
			continue
		}
		expanded, ok := expandInstallWord(word, vars)
		if !ok {
			return nil, "unresolved_variable", 0
		}
		if !safeInstallName(expanded) {
			return nil, "unsafe_name", 0
		}
		name, version := splitPackagePin(manager, expanded)
		rows = append(rows, Install{Manager: manager, Name: name, Version: version, LineNumber: line, Stage: stage})
	}
	if len(rows) == 0 {
		return nil, "missing_package", 0
	}
	return rows, "", 0
}

func splitPackagePin(manager, word string) (string, string) {
	if manager == "apt" || manager == "apt-get" || manager == "apk" {
		name, version, ok := strings.Cut(word, "=")
		if ok {
			return name, version
		}
	}
	if manager == "pecl" || manager == "pip" || manager == "pip3" {
		name, version, ok := strings.Cut(word, "==")
		if ok {
			return name, version
		}
		at := strings.LastIndex(word, "-")
		if manager == "pecl" && at > 0 && at+1 < len(word) && word[at+1] >= '0' && word[at+1] <= '9' {
			return word[:at], word[at+1:]
		}
	}
	if manager == "npm" {
		at := strings.LastIndex(word, "@")
		if at > 0 {
			return word[:at], word[at+1:]
		}
	}
	return word, ""
}

func safeInstallName(word string) bool {
	if word == "" || len(word) > 256 || strings.ContainsAny(word, ";&|<>`$\\\"' ") {
		return false
	}
	for _, char := range word {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func safeDownload(raw string) (string, bool, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", false, false
	}
	if parsed.User != nil || strings.ContainsAny(parsed.RawQuery, "\n\r") || hasTokenQuery(parsed.Query()) {
		return "", true, false
	}
	if parsed.Fragment != "" {
		return "", true, false
	}
	return parsed.Host + parsed.EscapedPath(), false, true
}

func hasTokenQuery(values url.Values) bool {
	for key := range values {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "token") || strings.Contains(lower, "key") || strings.Contains(lower, "secret") || strings.Contains(lower, "auth") || strings.Contains(lower, "password") {
			return true
		}
	}
	return false
}

func expandInstallWord(word string, vars map[string]string) (string, bool) {
	if !strings.Contains(word, "$") {
		return word, true
	}
	var out strings.Builder
	for i := 0; i < len(word); {
		if word[i] != '$' {
			out.WriteByte(word[i])
			i++
			continue
		}
		i++
		braced := i < len(word) && word[i] == '{'
		if braced {
			i++
		}
		start := i
		for i < len(word) && (word[i] == '_' || word[i] >= 'A' && word[i] <= 'Z' || word[i] >= 'a' && word[i] <= 'z' || word[i] >= '0' && word[i] <= '9') {
			i++
		}
		if start == i || braced && (i >= len(word) || word[i] != '}') {
			return "", false
		}
		name := word[start:i]
		if braced {
			i++
		}
		value, ok := vars[name]
		if !ok || strings.Contains(value, "$") || len(value) > 256 {
			return "", false
		}
		out.WriteString(value)
		if out.Len() > 256 {
			return "", false
		}
	}
	return out.String(), true
}

// shellSegments recognizes a bounded subset of shell words and && separators.
// It never invokes a shell or treats unsupported operators as install proof.
func shellSegments(raw string) ([][]string, bool) {
	segments := make([][]string, 0)
	for _, part := range strings.Split(raw, "&&") {
		words, ok := shellWords(part)
		if !ok {
			return nil, false
		}
		if len(words) > 0 {
			segments = append(segments, words)
		}
	}
	return segments, true
}

func shellWords(raw string) ([]string, bool) {
	words := make([]string, 0)
	var out strings.Builder
	var quote rune
	escaped := false
	for _, char := range raw {
		if escaped {
			out.WriteRune(char)
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
			} else {
				out.WriteRune(char)
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			continue
		}
		if unicode.IsSpace(char) {
			if out.Len() > 0 {
				words = append(words, out.String())
				out.Reset()
			}
			continue
		}
		if char == ';' || char == '|' || char == '<' || char == '>' || char == '`' {
			return nil, false
		}
		out.WriteRune(char)
	}
	if escaped || quote != 0 {
		return nil, false
	}
	if out.Len() > 0 {
		words = append(words, out.String())
	}
	return words, true
}
