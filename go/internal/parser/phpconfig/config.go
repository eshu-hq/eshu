// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package phpconfig extracts bounded, credential-guarded PHP server settings.
package phpconfig

import (
	"bufio"
	"bytes"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/secret/detect"
)

const (
	maxSourceBytes = 1 << 20
	maxLineBytes   = 2048
	maxRows        = 256
)

// Directive is a configuration setting proven by an admitted source file.
type Directive struct {
	Name    string
	Value   string
	Section string
	Line    int
}

// Mode identifies a server-mode declaration in source, not a running mode.
type Mode struct {
	Mode string
	Line int
}

// Skip records a discarded line without retaining its source or value.
type Skip struct {
	Line   int
	Reason string
}

// Result holds only bounded, guarded configuration evidence.
type Result struct {
	Directives []Directive
	Modes      []Mode
	Skipped    []Skip
	Dropped    int
	Unparsed   int
}

// Parse reads an admitted PHP or server configuration file without evaluation.
func Parse(path string, source []byte) Result {
	result := Result{}
	if len(source) > maxSourceBytes {
		result.skip(0, "file_too_large", false)
		return result
	}
	base := strings.ToLower(filepath.Base(path))
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	ini := base == "php.ini" || ((strings.Contains(normalized, "/conf.d/") || strings.HasPrefix(normalized, "conf.d/")) && strings.HasSuffix(base, ".ini"))
	htaccess := base == ".htaccess"
	fpm := strings.Contains(normalized, "php-fpm.d/") && strings.HasSuffix(base, ".conf")
	apache := strings.HasSuffix(base, ".conf") && !fpm
	if !ini && !htaccess && !fpm && !apache {
		return result
	}
	scanner := bufio.NewScanner(bytes.NewReader(source))
	scanner.Buffer(make([]byte, 0, 4096), maxLineBytes+1)
	section := ""
	line := 0
	for scanner.Scan() {
		line++
		raw := strings.TrimSpace(scanner.Text())
		if len(raw) > maxLineBytes {
			result.skip(line, "line_too_long", false)
			result.Directives = nil
			result.Modes = nil
			return result
		}
		if raw == "" || strings.HasPrefix(raw, ";") || strings.HasPrefix(raw, "#") {
			continue
		}
		if len(result.Directives)+len(result.Modes) >= maxRows {
			result.skip(line, "row_limit", false)
			result.Directives = nil
			result.Modes = nil
			return result
		}
		if strings.HasPrefix(raw, "[") {
			if !strings.HasSuffix(raw, "]") || len(raw) < 3 {
				result.skip(line, "invalid_section", false)
				continue
			}
			section = strings.TrimSpace(raw[1 : len(raw)-1])
			if sensitive(section, "") {
				result.skip(line, "credential_shape", true)
				section = ""
				continue
			}
			if section == "" || !safeWord(section) {
				result.skip(line, "invalid_section", false)
				section = ""
				continue
			}
			if fpm && !strings.EqualFold(section, "global") {
				result.Modes = append(result.Modes, Mode{Mode: "php-fpm", Line: line})
			}
			continue
		}
		if apache || htaccess {
			fields := strings.Fields(raw)
			if len(fields) >= 2 && strings.EqualFold(fields[0], "LoadModule") && strings.EqualFold(fields[1], "php_module") {
				result.Modes = append(result.Modes, Mode{Mode: "mod_php", Line: line})
				continue
			}
			if len(fields) > 0 && (strings.EqualFold(fields[0], "php_value") || strings.EqualFold(fields[0], "php_flag")) {
				if len(fields) < 3 {
					result.skip(line, "invalid_directive", false)
					continue
				}
				result.addDirective(line, fields[1], strings.Join(fields[2:], " "), "")
			}
			continue
		}
		if ini {
			name, value, ok := strings.Cut(raw, "=")
			if !ok {
				result.skip(line, "invalid_directive", false)
				continue
			}
			result.addDirective(line, strings.TrimSpace(name), stripINIComment(value), section)
		}
	}
	if scanner.Err() != nil {
		result.skip(line+1, "line_too_long", false)
		result.Directives = nil
		result.Modes = nil
	}
	return result
}

func (r *Result) addDirective(line int, name, value, section string) {
	name = strings.ToLower(strings.TrimSpace(name))
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '\'' || value[0] == '"') {
		if value[len(value)-1] != value[0] {
			r.skip(line, "unterminated_quote", false)
			return
		}
		value = value[1 : len(value)-1]
	} else if strings.ContainsAny(value, "\"'") {
		r.skip(line, "ambiguous_quote", false)
		return
	}
	if !safeWord(name) || value == "" || len(value) > 256 || strings.ContainsAny(value, "\r\n\x00") {
		r.skip(line, "invalid_directive", false)
		return
	}
	if sensitive(name, value) {
		r.skip(line, "credential_shape", true)
		return
	}
	r.Directives = append(r.Directives, Directive{Name: name, Value: value, Section: section, Line: line})
}

func (r *Result) skip(line int, reason string, dropped bool) {
	if dropped {
		r.Dropped++
	} else {
		r.Unparsed++
	}
	if len(r.Skipped) < maxRows {
		r.Skipped = append(r.Skipped, Skip{Line: line, Reason: reason})
	}
}

func safeWord(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, ch := range s {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '_', ch == '.', ch == '-':
		default:
			return false
		}
	}
	return true
}

func stripINIComment(value string) string {
	quote := byte(0)
	for i := 0; i < len(value); i++ {
		if (value[i] == '\'' || value[i] == '"') && (i == 0 || value[i-1] != '\\') {
			switch quote {
			case 0:
				quote = value[i]
			case value[i]:
				quote = 0
			}
		}
		if quote == 0 && (value[i] == ';' || value[i] == '#') && i > 0 && unicode.IsSpace(rune(value[i-1])) {
			return strings.TrimSpace(value[:i])
		}
	}
	return strings.TrimSpace(value)
}

func sensitive(name, value string) bool {
	if len(detect.Spans(name+"="+value)) > 0 {
		return true
	}
	lower := strings.ToLower(name + "=" + value)
	for _, part := range []string{"password", "passwd", "secret", "token", "api_key", "apikey", "private_key", "credential", "authorization", "bearer ", "user:pass@"} {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return strings.Contains(value, "://") && strings.Contains(strings.SplitN(value, "/", 4)[2], "@")
}
