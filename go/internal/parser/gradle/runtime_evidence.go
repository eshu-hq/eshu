// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gradle

import (
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

var runtimeSetting = regexp.MustCompile(`\b(?:(?:languageVersion\s*=\s*|languageVersion\.set\(\s*)JavaLanguageVersion\.of\(\s*([0-9]+)\s*\)\s*\)?|(?:sourceCompatibility|targetCompatibility)\s*=\s*(?:JavaVersion\.VERSION_([0-9]+)|['"]([^'"\r\n]+)['"]|([0-9]+))|jvmToolchain\(\s*([0-9]+)\s*\))`)

// ExtractRuntimeEvidence reads literal JVM settings from one supplied build
// script. It never executes Gradle or resolves variables across files.
func ExtractRuntimeEvidence(source []byte) shared.RuntimeEvidence {
	evidence := shared.NewRuntimeEvidence(source)
	if evidence.State == "over_cap" {
		return evidence
	}
	text := maskGradleRuntimeComments(string(source))
	matches := runtimeSetting.FindAllStringSubmatchIndex(text, -1)
	stack := make([]string, 0, 8)
	segment := 0
	line := 1
	quote := byte(0)
	matchIndex := 0
	for i := 0; i < len(text); i++ {
		for matchIndex < len(matches) && i >= matches[matchIndex][1] {
			matchIndex++
		}
		if matchIndex < len(matches) && i == matches[matchIndex][0] && quote == 0 {
			match := matches[matchIndex]
			value := ""
			kind := ""
			for group := 1; group <= 5; group++ {
				if match[group*2] < 0 {
					continue
				}
				value = text[match[group*2]:match[group*2+1]]
				switch group {
				case 1:
					if len(stack) == 2 && stack[len(stack)-2] == "java" && stack[len(stack)-1] == "toolchain" {
						kind = "gradle_java_toolchain"
					}
				case 2, 3, 4:
					if (len(stack) == 0 || (len(stack) == 1 && stack[0] == "java")) && (strings.TrimSpace(text[segment:i]) == "" || strings.TrimSpace(text[segment:i]) == "java.") {
						kind = "gradle_java_source_compatibility"
						if strings.HasPrefix(text[match[0]:match[1]], "targetCompatibility") {
							kind = "gradle_java_target_compatibility"
						}
					}
				case 5:
					if len(stack) == 1 && stack[0] == "kotlin" {
						kind = "gradle_kotlin_jvm_toolchain"
					}
				}
			}
			if kind != "" && runtimeSettingTerminated(text, match[1]) {
				evidence.Add(kind, "java", value, line)
			}
			for _, c := range text[i:match[1]] {
				if c == '\n' {
					line++
				}
			}
			i = match[1] - 1
			matchIndex++
			continue
		}
		c := text[i]
		if c == '\n' {
			line++
			if quote == 0 {
				segment = i + 1
			}
			continue
		}
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		switch c {
		case '{':
			header := strings.TrimSpace(text[segment:i])
			stack = append(stack, runtimeBlockName(header))
			segment = i + 1
		case '}':
			if len(stack) == 0 {
				evidence.State = "unavailable"
				evidence.Values = nil
				return evidence
			}
			stack = stack[:len(stack)-1]
			segment = i + 1
		case ';':
			segment = i + 1
		}
	}
	if len(stack) != 0 || quote != 0 {
		evidence.State = "unavailable"
		evidence.Values = nil
	}
	return evidence
}

func runtimeBlockName(header string) string {
	switch header {
	case "java", "toolchain", "kotlin":
		return header
	default:
		return ""
	}
}

// maskGradleRuntimeComments preserves offsets and line numbers while removing
// comments; quoted text stays so the literal setting scanner can read values.
func maskGradleRuntimeComments(source string) string {
	out := []byte(source)
	quote := byte(0)
	for i := 0; i < len(out); i++ {
		if quote != 0 {
			if out[i] == '\\' {
				i++
				continue
			}
			if out[i] == quote {
				quote = 0
			}
			continue
		}
		if out[i] == '\'' || out[i] == '"' {
			quote = out[i]
			continue
		}
		if i+1 >= len(out) || out[i] != '/' {
			continue
		}
		if out[i+1] == '/' {
			for i < len(out) && out[i] != '\n' {
				out[i] = ' '
				i++
			}
			if i >= len(out) {
				break
			}
		} else if out[i+1] == '*' {
			out[i], out[i+1] = ' ', ' '
			i += 2
			closed := false
			for i < len(out) {
				if i+1 < len(out) && out[i] == '*' && out[i+1] == '/' {
					out[i], out[i+1] = ' ', ' '
					i++
					closed = true
					break
				}
				if out[i] != '\n' {
					out[i] = ' '
				}
				i++
			}
			if !closed {
				break
			}
		}
	}
	return string(out)
}

func runtimeSettingTerminated(text string, end int) bool {
	for end < len(text) && (text[end] == ' ' || text[end] == '\t' || text[end] == '\r') {
		end++
	}
	return end == len(text) || text[end] == '\n' || text[end] == ';' || text[end] == '}'
}
