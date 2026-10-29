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

func parseInstallInstruction(item instruction, stage string, variables *installScope) ([]Install, []UnparsedCommand, int) {
	if item.heredoc {
		return parseInstallHeredoc(item, stage, variables)
	}
	if len(item.value) > maxInstallCommandBytes {
		return nil, []UnparsedCommand{{LineNumber: item.line, Reason: "command_over_limit"}}, 0
	}
	if item.keyword == "ADD" {
		return parseADDInstall(item, stage, variables)
	}
	runBody, reason, optionDrops := stripInstallRunOptions(item.value, variables)
	if reason != "" {
		return nil, []UnparsedCommand{{LineNumber: item.line, Reason: reason}}, optionDrops
	}
	segments, ok := shellSegments(runBody)
	if !ok {
		return nil, []UnparsedCommand{{LineNumber: item.line, Reason: "invalid_shell_words"}}, optionDrops
	}
	installs := make([]Install, 0)
	unparsed := make([]UnparsedCommand, 0)
	dropped := optionDrops
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

// stripInstallRunOptions handles the Dockerfile RUN flags before the shell
// command. Unsupported forms are recorded without interpreting the remainder.
func stripInstallRunOptions(raw string, vars *installScope) (string, string, int) {
	remaining := strings.TrimLeftFunc(raw, unicode.IsSpace)
	for strings.HasPrefix(remaining, "--") {
		end := strings.IndexFunc(remaining, unicode.IsSpace)
		if end < 0 {
			return "", "unsupported_run_option", countCredentialValues(strings.Fields(raw), vars)
		}
		word := remaining[:end]
		name, value, attached := strings.Cut(word, "=")
		switch name {
		case "--mount", "--network", "--security", "--device":
		default:
			return "", "unsupported_run_option", countCredentialValues(strings.Fields(raw), vars)
		}
		if !attached || value == "" || strings.ContainsAny(value, "\"'\\") {
			return "", "unsupported_run_option", countCredentialValues(strings.Fields(raw), vars)
		}
		if sensitiveInstallValue("", value, vars) {
			return "", "credential_run_option", countCredentialValues(strings.Fields(raw), vars)
		}
		remaining = strings.TrimLeftFunc(remaining[end:], unicode.IsSpace)
	}
	return remaining, "", 0
}

// isPythonModuleCommand reports whether command names a CPython interpreter
// that can carry `-m <manager>` installs: `python`, `python3`, or a dotted
// version such as `python3.11`. Anything else (including lookalikes such as
// `pythonfoo`) dispatches through the ordinary manager switch.
func isPythonModuleCommand(command string) bool {
	if command == "python" || command == "python3" {
		return true
	}
	if !strings.HasPrefix(command, "python") || len(command) == len("python") {
		return false
	}
	for _, r := range command[len("python"):] {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}

func parseInstallSegment(words []string, line int, stage string, vars *installScope) ([]Install, string, int) {
	if len(words) == 0 {
		return nil, "", 0
	}
	for _, word := range words {
		if strings.Contains(word, "$(") || strings.Contains(word, "`") {
			return nil, "unsupported_command", 0
		}
	}
	command := strings.TrimPrefix(words[0], "/usr/bin/")
	command = strings.TrimPrefix(command, "/bin/")
	for command == "sudo" {
		words = words[1:]
		if len(words) == 0 {
			return nil, "unsupported_command", 0
		}
		command = strings.TrimPrefix(words[0], "/usr/bin/")
		command = strings.TrimPrefix(command, "/bin/")
		if strings.HasPrefix(command, "-") {
			return nil, "unsupported_command", 0
		}
	}
	if isPythonModuleCommand(command) && len(words) >= 3 && words[1] == "-m" {
		return parseInstallSegment(words[2:], line, stage, vars)
	}
	manager, start := "", 0
	switch command {
	case "apt", "apt-get", "apk", "yum", "dnf", "pecl", "npm", "pip", "pip3":
		if len(words) < 2 {
			return nil, "missing_action", 0
		}
		actionIndex, reason, dropped := installActionIndex(command, words[1:], vars)
		if reason != "" {
			return nil, reason, dropped
		}
		if actionIndex >= len(words) {
			return nil, "missing_action", dropped
		}
		action := words[actionIndex]
		switch {
		case (command == "apt" || command == "apt-get" || command == "yum" || command == "dnf" || command == "pip" || command == "pip3" || command == "pecl") && action == "install":
		case command == "apk" && action == "add":
		case command == "npm" && action == "install":
		default:
			return nil, "", 0
		}
		manager, start = command, actionIndex+1
	case "docker-php-ext-install", "docker-php-ext-enable", "docker-php-ext-configure", "a2enmod":
		manager, start = command, 1
	case "curl", "wget":
		for _, word := range words[1:] {
			if !strings.Contains(word, "://") && !strings.Contains(word, "$") {
				continue
			}
			expanded, ok, sensitive := expandInstallWord(word, vars)
			if sensitive {
				return nil, "", 1
			}
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
	args := words[start:]
	if start > 2 {
		args = append(append([]string{}, words[1:start-1]...), args...)
	}
	options := parseInstallOptions(manager, args, vars)
	if options.reason != "" {
		return nil, options.reason, options.dropped
	}
	if manager == "npm" && !options.global {
		return nil, "", options.dropped
	}
	rows := make([]Install, 0, len(options.packages))
	for _, word := range options.packages {
		expanded, ok, sensitive := expandInstallWord(word, vars)
		if sensitive {
			return nil, "credential_name", options.dropped + 1
		}
		if !ok {
			return nil, "unresolved_variable", options.dropped
		}
		if credentialBearingValue(expanded) {
			return nil, "credential_name", options.dropped + 1
		}
		if !safeInstallName(expanded) {
			return nil, "unsafe_name", options.dropped
		}
		name, version := splitPackagePin(manager, expanded)
		rows = append(rows, Install{Manager: manager, Name: name, Version: version, LineNumber: line, Stage: stage})
	}
	if len(rows) == 0 {
		return nil, "missing_package", options.dropped
	}
	return rows, "", options.dropped
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
	if word == "" || len(word) > 256 || strings.ContainsAny(word, ";&|<>`$\\\"' ?%") || strings.Contains(word, "://") || strings.Contains(word, ":") && strings.Contains(word, "@") {
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
	if strings.ContainsAny(parsed.RawQuery, "\n\r") || credentialBearingURL(parsed) {
		return "", true, false
	}
	if parsed.Fragment != "" {
		return "", true, false
	}
	name := parsed.Host + parsed.EscapedPath()
	if len(name) > 256 {
		return "", false, false
	}
	return name, false, true
}

// hasTokenQuery rejects signed URL query keys, including bounded nested
// percent encoding. Parse errors are unsafe because Query silently drops them.
func hasTokenQuery(raw string) bool {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return true
	}
	for key := range values {
		for i := 0; i < 4; i++ {
			matched := false
			for _, token := range queryKeyTokens(key) {
				if sensitiveQueryTokens[token] {
					matched = true
					break
				}
			}
			if matched {
				return true
			}
			if !strings.Contains(key, "%") {
				break
			}
			decoded, err := url.PathUnescape(key)
			if err != nil || decoded == key {
				return true
			}
			key = decoded
		}
		if strings.Contains(key, "%") {
			return true
		}
	}
	return false
}

// expandInstallWord returns a separate sensitive outcome so callers can count
// a dropped credential without retaining its value in install evidence.
func expandInstallWord(word string, vars *installScope) (string, bool, bool) {
	if !strings.Contains(word, "$") {
		return word, true, false
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
			return "", false, false
		}
		name := word[start:i]
		if braced {
			i++
		}
		value, ok := vars.lookup(name)
		if credentialVariableName(name) || ok && value.sensitive {
			return "", false, true
		}
		if !ok || strings.Contains(value.text, "$") || len(value.text) > 256 {
			return "", false, false
		}
		out.WriteString(value.text)
		if out.Len() > 256 {
			return "", false, false
		}
	}
	return out.String(), true, false
}

// shellSegments recognizes a bounded subset of shell words and && separators.
// It never invokes a shell or treats unsupported operators as install proof.
func shellSegments(raw string) ([][]string, bool) {
	raw = stripShellComment(raw)
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

// stripShellComment removes a RUN shell comment only when # begins an
// unquoted word. Dockerfile ADD arguments use a different comment rule.
func stripShellComment(raw string) string {
	var quote rune
	escaped := false
	wordStart := true
	for index, char := range raw {
		if escaped {
			escaped = false
			wordStart = false
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
			wordStart = false
			continue
		}
		if unicode.IsSpace(char) || char == '&' {
			wordStart = true
			continue
		}
		if char == '#' && wordStart {
			return raw[:index]
		}
		wordStart = false
	}
	return raw
}
