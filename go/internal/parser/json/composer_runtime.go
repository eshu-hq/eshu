// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	stdjson "encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

const composerRuntimeMaxBytes = 1 << 20

// ComposerRuntimeValue is one string PHP declaration in a Composer document.
// The value is bounded before it leaves the parser and must pass the runtime
// declaration credential guard before it enters a fact.
type ComposerRuntimeValue struct {
	SourceKind string
	Value      string
	Line       int
}

// ComposerRuntimeEvidence records a complete parser outcome for one file.
// Rejected counts values that exceed the parser's handoff bound; Unavailable
// means the document or source position could not be proven.
type ComposerRuntimeEvidence struct {
	Values      []ComposerRuntimeValue
	Rejected    int
	Unavailable bool
	ByteSize    int
	Digest      string
}

// ExtractComposerRuntimeEvidence reads one Composer body through the same
// decoder and source-position helper used by Parse.
func ExtractComposerRuntimeEvidence(filename string, source []byte) (ComposerRuntimeEvidence, error) {
	result := ComposerRuntimeEvidence{ByteSize: len(source), Digest: composerRuntimeDigest(source)}
	if len(source) > composerRuntimeMaxBytes {
		result.Unavailable = true
		return result, nil
	}
	normalized, translate := normalizeJSONSource(source, filename)
	var document map[string]any
	if err := stdjson.Unmarshal([]byte(normalized), &document); err != nil {
		return result, err
	}
	result, err := composerRuntimeFromDocument(filename, document, []byte(normalized), buildTranslatedNewlineIndex(source, translate))
	result.ByteSize = len(source)
	result.Digest = composerRuntimeDigest(source)
	return result, err
}

func composerRuntimeFromDocument(filename string, document map[string]any, source []byte, idx *newlineIndex) (ComposerRuntimeEvidence, error) {
	result := ComposerRuntimeEvidence{ByteSize: len(source), Digest: composerRuntimeDigest(source)}
	if len(source) > composerRuntimeMaxBytes {
		result.Unavailable = true
		return result, nil
	}
	var sections []struct{ top, nested, kind string }
	switch filename {
	case "composer.json":
		sections = []struct{ top, nested, kind string }{
			{"require", "", "composer_json_require_php"},
			{"config", "platform", "composer_json_platform_php"},
		}
	case "composer.lock":
		sections = []struct{ top, nested, kind string }{
			{"platform", "", "composer_lock_platform_php"},
			{"platform-overrides", "", "composer_lock_platform_overrides_php"},
		}
	default:
		return result, nil
	}
	for _, section := range sections {
		top, ok := document[section.top]
		if !ok {
			continue
		}
		object, ok := top.(map[string]any)
		if !ok {
			continue
		}
		if section.nested != "" {
			object, ok = object[section.nested].(map[string]any)
			if !ok {
				continue
			}
		}
		value, exists := object["php"]
		if !exists {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if !shared.SafeVersionSpec(text) {
			result.Rejected++
			continue
		}
		raw, offset, found, err := jsonObjectExtractLastKey(source, section.top, 0)
		if err != nil || !found {
			result.Unavailable = true
			return result, err
		}
		if section.nested != "" {
			raw, offset, found, err = jsonObjectExtractLastKey(raw, section.nested, offset)
			if err != nil || !found {
				result.Unavailable = true
				return result, err
			}
		}
		lines, err := jsonObjectKeyLines(raw, offset, idx)
		if err != nil || lines["php"] < 1 {
			result.Unavailable = true
			return result, err
		}
		result.Values = append(result.Values, ComposerRuntimeValue{
			SourceKind: section.kind, Value: strings.Clone(text), Line: lines["php"],
		})
	}
	return result, nil
}

// jsonObjectExtractLastKey captures the last occurrence of a key, matching
// encoding/json's last-wins value policy while skipping unrelated values.
func jsonObjectExtractLastKey(data []byte, key string, baseOffset int64) (stdjson.RawMessage, int64, bool, error) {
	decoder := stdjson.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil {
		return nil, 0, false, err
	}
	if start != stdjson.Delim('{') {
		return nil, 0, false, fmt.Errorf("not a JSON object")
	}
	var last stdjson.RawMessage
	var offset int64
	found := false
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, 0, false, err
		}
		candidate, ok := token.(string)
		if !ok {
			return nil, 0, false, fmt.Errorf("invalid JSON key")
		}
		if candidate != key {
			var skip jsonValueSkipper
			if err := decoder.Decode(&skip); err != nil {
				return nil, 0, false, err
			}
			continue
		}
		var raw stdjson.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, 0, false, err
		}
		last = raw
		offset = baseOffset + decoder.InputOffset() - int64(len(raw))
		found = true
	}
	return last, offset, found, nil
}

func composerRuntimeDigest(source []byte) string {
	sum := sha1.Sum(source) // #nosec G401 -- matches the non-security snapshot content digest
	return hex.EncodeToString(sum[:])
}
