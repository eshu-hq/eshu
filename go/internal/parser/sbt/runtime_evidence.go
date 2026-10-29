// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import (
	"bytes"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// ExtractRuntimeEvidence returns the Scala declaration from the sbt scanner's
// existing source interpretation. Dynamic or conflicting settings emit none.
func ExtractRuntimeEvidence(source []byte) shared.RuntimeEvidence {
	evidence := shared.NewRuntimeEvidence(source)
	if evidence.State == "over_cap" {
		return evidence
	}
	scan := newScan(string(source))
	scan.run()
	return scan.runtimeEvidence(source)
}

func (s *scan) runtimeEvidence(source []byte) shared.RuntimeEvidence {
	evidence := shared.NewRuntimeEvidence(source)
	if evidence.State == "over_cap" {
		return evidence
	}
	if version := s.scalaVersion(); version != "" {
		evidence.Add("sbt_scala_version", "scala", version, s.scalaVersionLine)
	} else if s.scalaPoisoned {
		evidence.State = "unavailable"
	}
	return evidence
}

// splitPropertiesLine cuts a java.util.Properties line at its first
// separator: '=', ':', or whitespace. A whitespace separator may be
// followed by padding and one optional '=' or ':'. Backslash escapes are
// not unescaped; values are taken literally.
func splitPropertiesLine(trimmed string) (key, value string, ok bool) {
	if i := strings.IndexAny(trimmed, "=:"); i >= 0 {
		if ws := strings.IndexAny(trimmed, " \t\f"); ws < 0 || i < ws {
			return trimmed[:i], strings.TrimSpace(trimmed[i+1:]), true
		}
	}
	ws := strings.IndexAny(trimmed, " \t\f")
	if ws < 0 {
		return trimmed, "", false
	}
	rest := strings.TrimLeft(trimmed[ws:], " \t\f")
	if strings.HasPrefix(rest, "=") || strings.HasPrefix(rest, ":") {
		rest = rest[1:]
	}
	return trimmed[:ws], strings.TrimSpace(rest), true
}

// ExtractBuildPropertiesEvidence reads only sbt.version from a launcher pin.
// Other properties may contain credentials and are never returned.
func ExtractBuildPropertiesEvidence(source []byte) shared.RuntimeEvidence {
	evidence := shared.NewRuntimeEvidence(source)
	if evidence.State == "over_cap" {
		return evidence
	}
	seen := false
	for lineNumber, line := range bytes.Split(source, []byte{'\n'}) {
		trimmed := strings.TrimSpace(string(line))
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "!") {
			continue
		}
		key, value, ok := splitPropertiesLine(trimmed)
		if strings.TrimSpace(key) != "sbt.version" {
			continue
		}
		if !ok || seen || strings.TrimSpace(value) == "" {
			evidence.Values = nil
			evidence.State = "unavailable"
			return evidence
		}
		seen = true
		evidence.Add("sbt_build_properties_version", "sbt", value, lineNumber+1)
	}
	return evidence
}

// ParseBuildProperties returns a metadata-only parser payload for the sbt pin.
func ParseBuildProperties(path string, isDependency bool) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	payload := shared.BasePayload(path, "sbt_properties", isDependency)
	payload["jvm_runtime_evidence"] = ExtractBuildPropertiesEvidence(source)
	return payload, nil
}
