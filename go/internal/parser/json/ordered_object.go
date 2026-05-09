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
