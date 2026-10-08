// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

// TestOpenAPISpecHasNoDuplicateObjectKeys extends the path-key guard to every
// object in the served spec. A map decode resolves a duplicate key last-wins,
// so a route that declares "503" twice publishes only the later response and
// the earlier one drifts unseen; the dead-IaC and repository freshness routes
// each carried a second "503" that way (#7626).
func TestOpenAPISpecHasNoDuplicateObjectKeys(t *testing.T) {
	t.Parallel()

	duplicates, err := duplicateJSONObjectKeys(OpenAPISpec())
	if err != nil {
		t.Fatalf("walking served OpenAPI spec: %v", err)
	}
	for _, dup := range duplicates {
		t.Errorf("duplicate key in served OpenAPI spec: %s (a JSON decode keeps only the later value)", dup)
	}
}

// TestDuplicateJSONObjectKeysFindsPlantedDuplicates proves the walker reports
// a duplicate at any depth, with its path, and stays quiet on a clean document
// whose sibling objects reuse a key.
func TestDuplicateJSONObjectKeysFindsPlantedDuplicates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		doc  string
		want []string
	}{
		{"clean", `{"a": {"x": 1}, "b": {"x": 2}, "c": [{"x": 1}, {"x": 2}]}`, nil},
		{"top level", `{"a": 1, "a": 2}`, []string{`$["a"]`}},
		{
			"nested response", `{"paths": {"/r": {"post": {"responses": {"503": {}, "200": {}, "503": {}}}}}}`,
			[]string{`$["paths"]["/r"]["post"]["responses"]["503"]`},
		},
		{"inside array", `{"list": [{"k": 1}, {"k": 1, "k": 2}]}`, []string{`$["list"][1]["k"]`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := duplicateJSONObjectKeys(tc.doc)
			if err != nil {
				t.Fatalf("duplicateJSONObjectKeys() error = %v", err)
			}
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("duplicateJSONObjectKeys() = %q, want %q", got, tc.want)
			}
		})
	}
}

// duplicateJSONObjectKeys walks doc with a token decoder and returns the path
// of every key that repeats within one object.
func duplicateJSONObjectKeys(doc string) ([]string, error) {
	dec := json.NewDecoder(strings.NewReader(doc))
	var duplicates []string
	if err := walkJSONValue(dec, "$", &duplicates); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing data after the top-level value: %v", err)
	}
	return duplicates, nil
}

func walkJSONValue(dec *json.Decoder, path string, duplicates *[]string) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	switch tok {
	case json.Delim('{'):
		seen := make(map[string]bool)
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return fmt.Errorf("%s: reading key: %w", path, err)
			}
			key, ok := keyTok.(string)
			if !ok {
				return fmt.Errorf("%s: key is not a string: %v", path, keyTok)
			}
			child := fmt.Sprintf("%s[%q]", path, key)
			if seen[key] {
				*duplicates = append(*duplicates, child)
			}
			seen[key] = true
			if err := walkJSONValue(dec, child, duplicates); err != nil {
				return err
			}
		}
		_, err := dec.Token()
		return err
	case json.Delim('['):
		for i := 0; dec.More(); i++ {
			if err := walkJSONValue(dec, fmt.Sprintf("%s[%d]", path, i), duplicates); err != nil {
				return err
			}
		}
		_, err := dec.Token()
		return err
	default:
		return nil
	}
}
