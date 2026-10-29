// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// ExtractSetupCFGRuntimeEvidence reads the exact [options].python_requires
// field with ConfigParser's section, delimiter, comment and continuation
// rules. It does not evaluate interpolation or directives.
func ExtractSetupCFGRuntimeEvidence(source []byte) (PythonRuntimeEvidence, error) {
	evidence := PythonRuntimeEvidence{State: "parsed_empty", ByteSize: len(source), Digest: PythonRuntimeDigest(source)}
	if len(source) > pythonRuntimeMaxBytes {
		evidence.State = "over_cap"
		return evidence, nil
	}
	section := ""
	sections := make(map[string]bool)
	options := make(map[string]bool)
	var value string
	valueLine := 0
	valueIndent := -1
	lastOptionIsTarget := false
	lineNumber := 0
	for remaining := string(source); ; {
		line, tail, more := strings.Cut(remaining, "\n")
		lineNumber++
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && trimmed[0] != '#' && trimmed[0] != ';' {
			indent := len(line) - len(strings.TrimLeft(line, " \t"))
			switch {
			case strings.HasPrefix(trimmed, "["):
				end := strings.IndexByte(trimmed, ']')
				if end < 2 || strings.TrimSpace(trimmed[end+1:]) != "" {
					return unavailableSetupCFG(evidence, fmt.Errorf("invalid setup.cfg section at line %d", lineNumber))
				}
				section = trimmed[1:end]
				if sections[section] {
					return unavailableSetupCFG(evidence, fmt.Errorf("duplicate setup.cfg section at line %d", lineNumber))
				}
				sections[section] = true
				options = make(map[string]bool)
				valueIndent = -1
				lastOptionIsTarget = false
			case indent > valueIndent && valueIndent >= 0:
				if lastOptionIsTarget {
					value += " " + trimmed
				}
			default:
				index := strings.IndexAny(trimmed, "=:")
				if section == "" || index < 1 {
					return unavailableSetupCFG(evidence, fmt.Errorf("invalid setup.cfg option at line %d", lineNumber))
				}
				key := strings.ToLower(strings.TrimSpace(trimmed[:index]))
				if key == "" || options[key] {
					return unavailableSetupCFG(evidence, fmt.Errorf("duplicate or empty setup.cfg option at line %d", lineNumber))
				}
				options[key] = true
				valueIndent = indent
				lastOptionIsTarget = section == "options" && key == "python_requires"
				if lastOptionIsTarget {
					value = strings.TrimSpace(trimmed[index+1:])
					valueLine = lineNumber
				}
			}
		}
		if !more {
			break
		}
		remaining = tail
	}
	if valueLine == 0 {
		return evidence, nil
	}
	if !shared.SafeVersionSpec(value) {
		evidence.State = "parsed_values"
		evidence.Rejected = 1
		return evidence, nil
	}
	evidence.State = "parsed_values"
	evidence.Values = []PythonRuntimeValue{{SourceKind: "setup_cfg_python_requires", Value: strings.Clone(value), Line: valueLine}}
	return evidence, nil
}

func unavailableSetupCFG(evidence PythonRuntimeEvidence, err error) (PythonRuntimeEvidence, error) {
	evidence.State = "unavailable"
	evidence.Values = nil
	return evidence, err
}
