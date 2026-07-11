// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchhybrid

import (
	"encoding/json"
	"testing"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// TestDocumentTextCanonicalizesInvalidUTF8AcrossPersistence proves the hash
// input is stable across JSON persistence, which replaces invalid UTF-8 bytes
// with the Unicode replacement character.
func TestDocumentTextCanonicalizesInvalidUTF8AcrossPersistence(t *testing.T) {
	t.Parallel()

	original := searchdocs.Document{
		Title:       "File invalid.js",
		ContextText: "prefix\xffsuffix",
		Path:        "invalid.js",
		Labels:      []string{"language:javascript"},
	}
	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	var persisted searchdocs.Document
	if err := json.Unmarshal(payload, &persisted); err != nil {
		t.Fatalf("unmarshal document: %v", err)
	}

	if got := DocumentText(original); !utf8.ValidString(got) {
		t.Fatalf("DocumentText(original) contains invalid UTF-8: %q", got)
	}
	if got, want := DocumentContentHash(original), DocumentContentHash(persisted); got != want {
		t.Fatalf("content hash changed across persistence: got %q, want %q", got, want)
	}
}
