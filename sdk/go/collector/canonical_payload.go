// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

var errInvalidCanonicalPayload = errors.New("canonical payload has duplicate or invalid JSON members")

// decodeCanonicalPayload preserves number tokens and refuses duplicate decoded
// member names before a later member could replace an earlier value.
func decodeCanonicalPayload(raw []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decodeCanonicalValue(decoder, 0)
	if err != nil {
		return nil, errInvalidCanonicalPayload
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errInvalidCanonicalPayload
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errInvalidCanonicalPayload
	}
	return object, nil
}

func decodeCanonicalValue(decoder *json.Decoder, depth int) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, errInvalidCanonicalPayload
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return token, nil
	}
	// Match encoding/json's nesting bound; never recurse beyond its accepted JSON.
	if depth >= 10000 {
		return nil, errInvalidCanonicalPayload
	}
	switch delimiter {
	case '{':
		object := make(map[string]any)
		for decoder.More() {
			member, err := decoder.Token()
			if err != nil {
				return nil, errInvalidCanonicalPayload
			}
			key, ok := member.(string)
			if !ok {
				return nil, errInvalidCanonicalPayload
			}
			if _, exists := object[key]; exists {
				return nil, errInvalidCanonicalPayload
			}
			value, err := decodeCanonicalValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return nil, errInvalidCanonicalPayload
		}
		return object, nil
	case '[':
		array := make([]any, 0)
		for decoder.More() {
			value, err := decodeCanonicalValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return nil, errInvalidCanonicalPayload
		}
		return array, nil
	default:
		return nil, errInvalidCanonicalPayload
	}
}
