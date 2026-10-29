// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	stdjson "encoding/json"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func dotNetGlobalRuntimeEvidence(document map[string]any, normalized []byte, index *newlineIndex, original []byte) shared.DotNetRuntimeEvidence {
	evidence := shared.NewDotNetRuntimeEvidence(original)
	if evidence.State == "over_cap" {
		return evidence
	}
	sdk, ok := document["sdk"].(map[string]any)
	if !ok {
		return evidence
	}
	value, ok := sdk["version"].(string)
	if !ok {
		return evidence
	}
	raw, offset, found, err := jsonObjectExtractLastKey(normalized, "sdk", 0)
	if err != nil || !found {
		evidence.State = "unavailable"
		return evidence
	}
	sdkRaw := stdjson.RawMessage(raw)
	lines, err := jsonObjectKeyLines(sdkRaw, offset, index)
	if err != nil || lines["version"] < 1 {
		evidence.State = "unavailable"
		return evidence
	}
	evidence.Add("dotnet_global_json_sdk", value, lines["version"])
	return evidence
}

// ExtractDotNetGlobalEvidence parses a supplied global.json body for the content-only path.
func ExtractDotNetGlobalEvidence(source []byte) shared.DotNetRuntimeEvidence {
	evidence := shared.NewDotNetRuntimeEvidence(source)
	if evidence.State == "over_cap" {
		return evidence
	}
	normalizedSource := shared.NormalizeLineEndings(source)
	normalized, translate := normalizeJSONSource(normalizedSource, "global.json")
	var document map[string]any
	if err := stdjson.Unmarshal([]byte(normalized), &document); err != nil {
		evidence.State = "unavailable"
		return evidence
	}
	return dotNetGlobalRuntimeEvidence(document, []byte(normalized), buildTranslatedNewlineIndex(normalizedSource, translate), source)
}
