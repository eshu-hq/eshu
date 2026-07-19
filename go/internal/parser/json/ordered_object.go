// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// orderedJSONEntry is one key/value pair from a source-order JSON object
// walk. Line is the 1-based source line of the key token, real (not
// synthesized) whenever the entry was produced through unmarshalOrderedJSONObjectAt
// with a non-nil newlineIndex; it is 0 when no index was supplied (the plain
// unmarshalOrderedJSONObject wrapper below, kept for callers that only need
// key order). valueStartByte is the global byte offset where Value begins in
// the original source buffer; callers recursing into a nested object (see
// orderedJSONNestedObjectAt) pass it back in as the next call's baseOffset so
// line numbers stay correct at any nesting depth.
type orderedJSONEntry struct {
	Key            string
	Value          json.RawMessage
	Line           int
	valueStartByte int64
}

// unmarshalOrderedJSONObject walks data (assumed to start at byte offset 0 of
// its own buffer) and returns its top-level entries in source order, without
// computing line numbers. Callers that need real line numbers must use
// unmarshalOrderedJSONObjectAt with a newlineIndex built over the true
// top-level source buffer instead.
func unmarshalOrderedJSONObject(data []byte) ([]orderedJSONEntry, error) {
	return unmarshalOrderedJSONObjectAt(data, 0, nil)
}

// unmarshalOrderedJSONObjectAt is unmarshalOrderedJSONObject plus real source
// line numbers. baseOffset is the byte offset of data[0] within the original
// top-level source buffer idx was built from (0 for a top-level call; a
// nested entry's valueStartByte for a recursive call). idx may be nil, in
// which case Line is left at its zero value for every entry (used by the
// plain unmarshalOrderedJSONObject wrapper and tests that only need key
// order).
//
// Line capture relies on decoder.InputOffset(): it reports the offset
// immediately after the most recently returned token. Reading it right after
// decoder.Token() returns the key gives the offset just past the key
// string's closing quote; JSON string tokens cannot contain a raw newline
// (an embedded newline must be the two-byte escape \n), so the key's start
// and end byte always share one source line, and offset-after-key resolves
// to the same line as offset-at-key-start. This deliberately assigns the
// *this* entity's key its own line rather than the line of the last nested
// key decoded underneath it.
func unmarshalOrderedJSONObjectAt(data []byte, baseOffset int64, idx *newlineIndex) ([]orderedJSONEntry, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("read json object start: %w", err)
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return nil, fmt.Errorf("json value is not an object")
	}

	entries := make([]orderedJSONEntry, 0)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("read json object key: %w", err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, fmt.Errorf("json object key has type %T, want string", keyToken)
		}
		keyEndOffset := decoder.InputOffset()

		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, fmt.Errorf("decode json object value for %q: %w", key, err)
		}
		valueEndOffset := decoder.InputOffset()
		valueStartOffset := valueEndOffset - int64(len(raw))

		entry := orderedJSONEntry{
			Key:            key,
			Value:          raw,
			valueStartByte: baseOffset + valueStartOffset,
		}
		if idx != nil {
			entry.Line = idx.lineAt(baseOffset + keyEndOffset)
		}
		entries = append(entries, entry)
	}

	endToken, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("read json object end: %w", err)
	}
	endDelim, ok := endToken.(json.Delim)
	if !ok || endDelim != '}' {
		return nil, fmt.Errorf("json object end token = %v, want }", endToken)
	}
	return entries, nil
}

func orderedJSONKeys(entries []orderedJSONEntry) []string {
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, entry.Key)
	}
	return keys
}

func orderedJSONNestedObject(entries []orderedJSONEntry, key string) ([]orderedJSONEntry, bool, error) {
	return orderedJSONNestedObjectAt(entries, key, nil)
}

// orderedJSONNestedObjectAt is orderedJSONNestedObject plus real line
// numbers: it re-decodes the matching entry's Value using that entry's
// valueStartByte as the base offset, so lines computed for the nested
// entries stay correct against the original top-level source buffer idx was
// built from.
func orderedJSONNestedObjectAt(entries []orderedJSONEntry, key string, idx *newlineIndex) ([]orderedJSONEntry, bool, error) {
	for _, entry := range entries {
		if entry.Key != key {
			continue
		}
		nested, err := unmarshalOrderedJSONObjectAt(entry.Value, entry.valueStartByte, idx)
		if err != nil {
			return nil, false, err
		}
		return nested, true, nil
	}
	return nil, false, nil
}

// orderedJSONSectionEntries returns the entries nested under key inside
// entries with real Line numbers, or (nil, false) when entries is empty (the
// ordered walk was unavailable for this file) or key is missing/not an
// object. Callers must fall back to a sorted-map iteration and omit
// line_number when this returns false: without an ordered walk there is no
// real source position to report, and reporting one would fabricate it.
func orderedJSONSectionEntries(entries []orderedJSONEntry, key string, idx *newlineIndex) ([]orderedJSONEntry, bool) {
	if len(entries) == 0 {
		return nil, false
	}
	nested, ok, err := orderedJSONNestedObjectAt(entries, key, idx)
	if err != nil || !ok {
		return nil, false
	}
	return nested, true
}

// orderedJSONSectionLines returns a name->line map for the entries nested
// under key, or nil when no ordered/real position data is available. It is
// the by-name counterpart of orderedJSONSectionEntries for callers that walk
// a fallback map[string]any by key name rather than the ordered entry slice
// directly.
func orderedJSONSectionLines(entries []orderedJSONEntry, key string, idx *newlineIndex) map[string]int {
	nested, ok := orderedJSONSectionEntries(entries, key, idx)
	if !ok {
		return nil
	}
	lines := make(map[string]int, len(nested))
	for _, entry := range nested {
		lines[entry.Key] = entry.Line
	}
	return lines
}

// orderedJSONEntryLine returns the source line of the top-level entry named
// key within entries, or (0, false) when key is absent or entries carries no
// real line data.
func orderedJSONEntryLine(entries []orderedJSONEntry, key string) (int, bool) {
	for _, entry := range entries {
		if entry.Key == key {
			return entry.Line, entry.Line > 0
		}
	}
	return 0, false
}

// orderedJSONEntryRaw returns the raw value bytes and global start offset of
// the top-level entry named key within entries, or (nil, 0, false) when key
// is absent. Callers use the returned (raw, start) pair as the (data,
// baseOffset) arguments to unmarshalOrderedJSONArrayLines or a recursive
// unmarshalOrderedJSONObjectAt call, so a nested array or object under a
// top-level key gets real per-element/per-key lines without re-scanning the
// whole document.
func orderedJSONEntryRaw(entries []orderedJSONEntry, key string) (json.RawMessage, int64, bool) {
	for _, entry := range entries {
		if entry.Key == key {
			return entry.Value, entry.valueStartByte, true
		}
	}
	return nil, 0, false
}

// unmarshalOrderedJSONArrayLines walks a JSON array and returns the 1-based
// source line each element starts on, in array order. data is the array's
// raw bytes (for example from orderedJSONEntryRaw); baseOffset locates
// data[0] within the buffer idx was built from. It mirrors
// unmarshalOrderedJSONObjectAt's offset math (end-offset-minus-raw-length)
// but for array elements, which have no key token to anchor on.
func unmarshalOrderedJSONArrayLines(data []byte, baseOffset int64, idx *newlineIndex) ([]int, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("read json array start: %w", err)
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '[' {
		return nil, fmt.Errorf("json value is not an array")
	}

	lines := make([]int, 0)
	for decoder.More() {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, fmt.Errorf("decode json array element: %w", err)
		}
		endOffset := decoder.InputOffset()
		startOffset := endOffset - int64(len(raw))
		lines = append(lines, idx.lineAt(baseOffset+startOffset))
	}

	endToken, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("read json array end: %w", err)
	}
	endDelim, ok := endToken.(json.Delim)
	if !ok || endDelim != ']' {
		return nil, fmt.Errorf("json array end token = %v, want ]", endToken)
	}
	return lines, nil
}

// jsonObjectKeyLines performs a single-level scan over the JSON object in
// data, returning each key's source line without materializing any value's
// bytes (it discards values through jsonValueSkipper, the same no-op
// json.Unmarshaler topLevelJSONKeyOrder uses). Use this for large flat
// sections — package-lock.json's "packages"/"dependencies", pipfile.lock's
// "default"/"develop", a nuget target-framework's package map — where only a
// name->line lookup is needed and the #4873 lockfile-performance rule against
// copying every value into a json.RawMessage still applies.
func jsonObjectKeyLines(data []byte, baseOffset int64, idx *newlineIndex) (map[string]int, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("read json object start: %w", err)
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return nil, fmt.Errorf("json value is not an object")
	}

	lines := make(map[string]int)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("read json object key: %w", err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, fmt.Errorf("json object key has type %T, want string", keyToken)
		}
		lines[key] = idx.lineAt(baseOffset + decoder.InputOffset())

		var skip jsonValueSkipper
		if err := decoder.Decode(&skip); err != nil {
			return nil, fmt.Errorf("skip json object value for %q: %w", key, err)
		}
	}

	endToken, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("read json object end: %w", err)
	}
	endDelim, ok := endToken.(json.Delim)
	if !ok || endDelim != '}' {
		return nil, fmt.Errorf("json object end token = %v, want }", endToken)
	}
	return lines, nil
}

// jsonObjectExtractKey scans the top level of the JSON object in data for
// key and, when found, returns its raw value bytes plus the value's global
// start offset (data's own byte 0 is treated as baseOffset). It materializes
// a json.RawMessage only for the matched key — every other top-level value
// is discarded via jsonValueSkipper — so locating one nested section (for
// example "packages" inside a large composer.lock) stays cheap even when
// sibling top-level keys carry large values.
func jsonObjectExtractKey(data []byte, key string, baseOffset int64) (json.RawMessage, int64, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, 0, false, fmt.Errorf("read json object start: %w", err)
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return nil, 0, false, fmt.Errorf("json value is not an object")
	}

	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, 0, false, fmt.Errorf("read json object key: %w", err)
		}
		candidateKey, ok := keyToken.(string)
		if !ok {
			return nil, 0, false, fmt.Errorf("json object key has type %T, want string", keyToken)
		}
		if candidateKey != key {
			var skip jsonValueSkipper
			if err := decoder.Decode(&skip); err != nil {
				return nil, 0, false, fmt.Errorf("skip json object value for %q: %w", candidateKey, err)
			}
			continue
		}

		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, 0, false, fmt.Errorf("decode json object value for %q: %w", candidateKey, err)
		}
		endOffset := decoder.InputOffset()
		startOffset := endOffset - int64(len(raw))
		return raw, baseOffset + startOffset, true, nil
	}
	return nil, 0, false, nil
}

// jsonFilenameNeedsOrderedEntries reports whether Parse's dispatch for
// filename reads topLevelEntries. It mirrors the exact filenames the
// package.json/composer.json/tsconfig*.json branch of Parse's switch
// handles (guarded the same way, by shouldSkipJSONEntities first), so the
// two must stay in lockstep: dependencyVariablesWithScope,
// jsonScriptFunctions, and tsconfigVariables are the only functions in this
// package that read ordered nested entries (dependency/script emission
// order, compilerOptions.paths order) rather than just the top-level key
// names.
func jsonFilenameNeedsOrderedEntries(filename string) bool {
	if shouldSkipJSONEntities(filename) {
		return false
	}
	return filename == "package.json" || filename == "composer.json" || isTypeScriptConfigFilename(filename)
}

// jsonValueSkipper is a no-op json.Unmarshaler. Decoding a value into it
// makes encoding/json locate and skip that value's byte range without the
// defensive copy json.RawMessage.UnmarshalJSON performs
// (`append((*m)[0:0], data...)`). unmarshalOrderedJSONObject needs that copy
// because callers (orderedJSONNestedObject) re-decode a captured entry's
// bytes later to recover nested key order. topLevelJSONKeyOrder only needs
// the key names, so it can discard each value's bytes instead of copying
// them.
type jsonValueSkipper struct{}

// UnmarshalJSON discards data. It exists only so json.Decoder.Decode treats
// jsonValueSkipper as a json.Unmarshaler and hands it the value's byte range
// instead of decoding into a Go value tree.
func (*jsonValueSkipper) UnmarshalJSON(_ []byte) error { return nil }

// topLevelJSONKeyOrder returns the top-level object keys of data in source
// order without materializing their values. Use it where only the top-level
// key sequence is needed (json_metadata.top_level_keys) and no caller reads
// nested key order for this document — the dedicated lockfile parsers
// (package-lock.json, packages.lock.json, composer.lock, Pipfile.lock,
// Package.resolved), CloudFormation templates, dbt manifests, and any other
// JSON file that does not reach package.json/composer.json/tsconfig*.json
// handling in Parse. For those three filenames, callers need ordered nested
// entries (dependency/script emission order) and must keep using
// unmarshalOrderedJSONObject instead.
//
// data must already be known-valid JSON whose root value is an object (Parse
// establishes this via the initial `any` decode before calling this
// function); on malformed input or a non-object root this returns an error
// that callers may treat the same way unmarshalOrderedJSONObject's error is
// treated today: skip populating the ordered metadata rather than fail the
// parse.
func topLevelJSONKeyOrder(data []byte) ([]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("read json object start: %w", err)
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return nil, fmt.Errorf("json value is not an object")
	}

	keys := make([]string, 0)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("read json object key: %w", err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, fmt.Errorf("json object key has type %T, want string", keyToken)
		}
		var skip jsonValueSkipper
		if err := decoder.Decode(&skip); err != nil {
			return nil, fmt.Errorf("skip json object value for %q: %w", key, err)
		}
		keys = append(keys, key)
	}

	endToken, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("read json object end: %w", err)
	}
	endDelim, ok := endToken.(json.Delim)
	if !ok || endDelim != '}' {
		return nil, fmt.Errorf("json object end token = %v, want }", endToken)
	}
	return keys, nil
}
