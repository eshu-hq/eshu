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

// parse reads the raw version tokens of one pin file body.
func parse(sourceKind, body string) []declaration {
	lines := strings.Split(strings.TrimPrefix(body, "\xef\xbb\xbf"), "\n")
	switch sourceKind {
	case sourceKindToolVersions:
		return parseToolVersions(lines)
	case sourceKindSdkmanrc:
		return parseSdkmanrc(lines)
	case sourceKindRustToolchain:
		return parseRustToolchain(lines)
	}
	runtime := fileRuntime[sourceKind]
	if runtime == "" {
		return nil
	}
	// pyenv reads every line; the other tools read only the first effective
	// line, so a trailing note never becomes a declaration.
	allLines := sourceKind == sourceKindPythonVersion
	var out []declaration
	for i, line := range lines {
		tokens := splitTokens(stripComment(line))
		if len(tokens) == 0 {
			continue
		}
		for order, token := range tokens {
			out = append(out, declaration{line: i + 1, order: order, runtime: runtime, rawSpec: token})
		}
		if !allLines {
			break
		}
	}
	return out
}

func parseToolVersions(lines []string) []declaration {
	var out []declaration
	for i, line := range lines {
		tokens := splitTokens(stripComment(line))
		if len(tokens) < 2 {
			continue
		}
		runtime, tool := normalizeRuntime(tokens[0])
		for order, token := range tokens[1:] {
			out = append(out, declaration{line: i + 1, order: order, runtime: runtime, toolName: tool, rawSpec: token})
		}
	}
	return out
}

func parseSdkmanrc(lines []string) []declaration {
	var out []declaration
	for i, line := range lines {
		key, value, ok := strings.Cut(stripComment(line), "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || key == "" || value == "" {
			continue
		}
		runtime, tool := normalizeRuntime(key)
		d := declaration{line: i + 1, runtime: runtime, toolName: tool, rawSpec: value}
		if runtime == "java" {
			if m := sdkmanDistribution.FindStringSubmatch(value); m != nil {
				dist := m[2]
				d.distribution = &dist
			}
		}
		out = append(out, d)
	}
	return out
}

// parseRustToolchain reads the channel of a rust-toolchain file. A TOML body
// declares a version only through its channel key; the legacy plain form is
// one channel or release on the first effective line.
func parseRustToolchain(lines []string) []declaration {
	tomlMode := false
	for _, line := range lines {
		if tomlLikePattern.MatchString(stripComment(line)) {
			tomlMode = true
			break
		}
	}
	for i, line := range lines {
		if tomlMode {
			if m := tomlChannelPattern.FindStringSubmatch(stripComment(line)); m != nil {
				return []declaration{{line: i + 1, runtime: "rust", rawSpec: strings.TrimSpace(m[1])}}
			}
			continue
		}
		if tokens := splitTokens(stripComment(line)); len(tokens) > 0 {
			return []declaration{{line: i + 1, runtime: "rust", rawSpec: tokens[0]}}
		}
	}
	return nil
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
