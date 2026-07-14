// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type orderedJSONEntry struct {
	Key   string
	Value json.RawMessage
}

func unmarshalOrderedJSONObject(data []byte) ([]orderedJSONEntry, error) {
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

		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, fmt.Errorf("decode json object value for %q: %w", key, err)
		}
		entries = append(entries, orderedJSONEntry{
			Key:   key,
			Value: raw,
		})
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
	for _, entry := range entries {
		if entry.Key != key {
			continue
		}
		nested, err := unmarshalOrderedJSONObject(entry.Value)
		if err != nil {
			return nil, false, err
		}
		return nested, true, nil
	}
	return nil, false, nil
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
