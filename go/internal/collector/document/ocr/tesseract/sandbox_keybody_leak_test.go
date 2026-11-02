// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"encoding/json"
	"testing"
)

// TestTextInPersistedSeesEscapedCharacters pins the leak check of the key-body
// proof (#9799): a body line that holds a character encoding/json escapes must
// still be found when it is persisted in clear.
func TestTextInPersistedSeesEscapedCharacters(t *testing.T) {
	t.Parallel()

	lines := []string{
		"Ab3dEf6hIj9lMn2pQr5tUv8x&Yz1bCd4eFg7hJk0m",
		"Ab3dEf6hIj9l<n2pQr5tUv8xYz1bCd4eFg7hJk0m",
		"Ab3dEf6hIj9lMn2pQr5tUv8xYz1bCd4eFg>hJk0m",
		`Ab3dEf6hIj9lMn2pQr5t"v8xYz1bCd4eFg7hJk0m`,
		`Ab3dEf6hIj9lMn2pQr5t\v8xYz1bCd4eFg7hJk0m`,
		"Ab3dEf6hIj9lMn2pQr5tUv8xYz1bCd4eFg7hJk0m",
	}
	for _, line := range lines {
		persisted, err := json.Marshal(map[string]string{"content": line})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !textInPersisted(persisted, line) {
			t.Errorf("line %q persisted in clear as %s was not found", line, persisted)
		}
		if textInPersisted(persisted, line+"X") {
			t.Errorf("line %q+X was found, want no match", line)
		}
	}
}
