// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	stdjson "encoding/json"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// dartFvmrcRuntimeEvidence reads the flutter pin from an .fvmrc document
// once, keeping only a credential-guarded version string with its real
// source line. Other keys never reach runtime facts.
func dartFvmrcRuntimeEvidence(document map[string]any, normalized []byte, index *newlineIndex, original []byte) shared.RuntimeEvidence {
	evidence := shared.NewRuntimeEvidence(original)
	if evidence.State == "over_cap" {
		return evidence
	}
	value, ok := document["flutter"].(string)
	if !ok {
		return evidence
	}
	entries, err := unmarshalOrderedJSONObjectAt(normalized, 0, index)
	if err != nil {
		evidence.State = "unavailable"
		return evidence
	}
	line := 0
	for _, entry := range entries {
		if entry.Key == "flutter" {
			line = entry.Line
		}
	}
	if line < 1 {
		evidence.State = "unavailable"
		return evidence
	}
	evidence.Add("dart_fvmrc_flutter", "dart", value, line)
	return evidence
}

// ExtractDartFvmrcEvidence parses a supplied .fvmrc body for the content-only path.
func ExtractDartFvmrcEvidence(source []byte) shared.RuntimeEvidence {
	evidence := shared.NewRuntimeEvidence(source)
	if evidence.State == "over_cap" {
		return evidence
	}
	normalizedSource := shared.NormalizeLineEndings(source)
	normalized, translate := normalizeJSONSource(normalizedSource, ".fvmrc")
	var document map[string]any
	if err := stdjson.Unmarshal([]byte(normalized), &document); err != nil {
		evidence.State = "unavailable"
		return evidence
	}
	return dartFvmrcRuntimeEvidence(document, []byte(normalized), buildTranslatedNewlineIndex(normalizedSource, translate), source)
}
