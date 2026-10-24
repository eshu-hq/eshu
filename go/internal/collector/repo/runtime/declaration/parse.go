// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"regexp"
	"strings"
)

// declaration is one raw version token read from a pin file, before it is
// classified.
type declaration struct {
	line, order  int
	runtime      string
	toolName     *string
	rawSpec      string
	distribution *string
}

// fileRuntime is the runtime a single-runtime pin file declares.
var fileRuntime = map[string]string{
	sourceKindNvmrc:            "node",
	sourceKindNodeVersion:      "node",
	sourceKindPythonVersion:    "python",
	sourceKindRubyVersion:      "ruby",
	sourceKindJavaVersion:      "java",
	sourceKindGoVersion:        "go",
	sourceKindTerraformVersion: "terraform",
	sourceKindRustToolchain:    "rust",
}

// runtimeAliases normalises a tool token to the canonical runtime name. An
// unknown tool keeps its lower-cased token, so the runtime set stays open.
var runtimeAliases = map[string]string{
	"nodejs": "node",
	"golang": "go",
}

var (
	tomlChannelPattern = regexp.MustCompile(`^\s*channel\s*=\s*["']([^"']+)["']`)
	tomlLikePattern    = regexp.MustCompile(`^\s*(\[|[A-Za-z_][\w-]*\s*=)`)
	sdkmanDistribution = regexp.MustCompile(`^(\d[\d.]*)-([A-Za-z][A-Za-z0-9]*)$`)
)

// normalizeRuntime returns the canonical runtime for a tool token and, when it
// differed from the token as written, the verbatim token.
func normalizeRuntime(tool string) (string, *string) {
	lower := strings.ToLower(tool)
	runtime := lower
	if alias, ok := runtimeAliases[lower]; ok {
		runtime = alias
	}
	if runtime == tool {
		return runtime, nil
	}
	verbatim := tool
	return runtime, &verbatim
}

// declarationSink collects the declarations of one pin file. It stores at most
// limit of them and counts every one, so a file over the token cap costs
// memory bounded by limit rather than by its token count, and the skip log can
// still report the exact count.
type declarationSink struct {
	limit int
	count int
	out   []declaration
}

// add counts d and keeps it while the sink is under its limit.
func (s *declarationSink) add(d declaration) {
	s.count++
	if s.count <= s.limit {
		s.out = append(s.out, d)
	}
}

// eachLine calls fn with the 1-based number and text of every "\n"-separated
// line of body, in order, until fn returns false. It walks the body in place
// and allocates nothing, unlike strings.Split, which allocates a slice header
// per line.
func eachLine(body string, fn func(number int, line string) bool) {
	number := 1
	for {
		end := strings.IndexByte(body, '\n')
		if end < 0 {
			fn(number, body)
			return
		}
		if !fn(number, body[:end]) {
			return
		}
		body = body[end+1:]
		number++
	}
}

// parse reads the raw version tokens of one pin file body. It returns at most
// limit declarations and total, the number of tokens the file holds, so total >
// limit tells the caller the file is over the cap while memory stays bounded by
// limit and the longest line.
func parse(sourceKind, body string, limit int) (declarations []declaration, total int) {
	body = strings.TrimPrefix(body, "\xef\xbb\xbf")
	sink := &declarationSink{limit: limit}
	switch sourceKind {
	case sourceKindToolVersions:
		parseToolVersions(body, sink)
	case sourceKindSdkmanrc:
		parseSdkmanrc(body, sink)
	case sourceKindRustToolchain:
		parseRustToolchain(body, sink)
	default:
		parseSingleRuntime(sourceKind, body, sink)
	}
	return sink.out, sink.count
}

// parseSingleRuntime reads a pin file that names one runtime, such as .nvmrc.
func parseSingleRuntime(sourceKind, body string, sink *declarationSink) {
	runtime := fileRuntime[sourceKind]
	if runtime == "" {
		return
	}
	// pyenv reads every line; the other tools read only the first effective
	// line, so a trailing note never becomes a declaration.
	allLines := sourceKind == sourceKindPythonVersion
	eachLine(body, func(number int, line string) bool {
		seen := false
		eachToken(stripComment(line), func(order int, token string) bool {
			seen = true
			sink.add(declaration{line: number, order: order, runtime: runtime, rawSpec: token})
			return true
		})
		return !seen || allLines
	})
}

func parseToolVersions(body string, sink *declarationSink) {
	eachLine(body, func(number int, line string) bool {
		var runtime string
		var tool *string
		eachToken(stripComment(line), func(index int, token string) bool {
			if index == 0 {
				runtime, tool = normalizeRuntime(token)
				return true
			}
			sink.add(declaration{line: number, order: index - 1, runtime: runtime, toolName: tool, rawSpec: token})
			return true
		})
		return true
	})
}

func parseSdkmanrc(body string, sink *declarationSink) {
	eachLine(body, func(number int, line string) bool {
		key, value, ok := strings.Cut(stripComment(line), "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || key == "" || value == "" {
			return true
		}
		runtime, tool := normalizeRuntime(key)
		d := declaration{line: number, runtime: runtime, toolName: tool, rawSpec: value}
		if runtime == "java" {
			if m := sdkmanDistribution.FindStringSubmatch(value); m != nil {
				dist := m[2]
				d.distribution = &dist
			}
		}
		sink.add(d)
		return true
	})
}

// parseRustToolchain reads the channel of a rust-toolchain file. A TOML body
// declares a version only through its channel key; the legacy plain form is
// one channel or release on the first effective line.
func parseRustToolchain(body string, sink *declarationSink) {
	tomlMode := false
	eachLine(body, func(_ int, line string) bool {
		if tomlLikePattern.MatchString(stripComment(line)) {
			tomlMode = true
			return false
		}
		return true
	})
	eachLine(body, func(number int, line string) bool {
		if tomlMode {
			if m := tomlChannelPattern.FindStringSubmatch(stripComment(line)); m != nil {
				sink.add(declaration{line: number, runtime: "rust", rawSpec: strings.TrimSpace(m[1])})
				return false
			}
			return true
		}
		found := false
		eachToken(stripComment(line), func(_ int, token string) bool {
			sink.add(declaration{line: number, runtime: "rust", rawSpec: token})
			found = true
			return false
		})
		return !found
	})
}

// stripComment removes a trailing "#" comment and surrounding whitespace,
// including a CR from CRLF line endings.
func stripComment(line string) string {
	line = strings.TrimRight(line, "\r")
	for i := 0; i < len(line); i++ {
		if line[i] == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
			line = line[:i]
			break
		}
	}
	return strings.TrimSpace(line)
}
