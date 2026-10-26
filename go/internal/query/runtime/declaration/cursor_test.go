// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"
	"testing"
)

// TestCursorLongPathRoundTrip pins the #8817 P2: a cursor this route issues
// for a row with a long relative_path must decode on the next call, or the
// page walk dead-ends at that row.
func TestCursorLongPathRoundTrip(t *testing.T) {
	t.Parallel()

	key := cursorKey{
		Version:      cursorVersion,
		RepoID:       "repository:r_a",
		RelativePath: strings.Repeat("d/", 750) + ".nvmrc",
		LineNumber:   3,
		Runtime:      "node",
		OrderIndex:   2,
	}
	token, err := encodeCursor(key)
	if err != nil {
		t.Fatalf("encodeCursor() error = %v", err)
	}
	if len(token) <= 2048 {
		t.Fatalf("probe token is %d chars, want over the old 2048 cap", len(token))
	}
	got, err := decodeCursor(token)
	if err != nil {
		t.Fatalf("decodeCursor(self-issued %d-char token) error = %v", len(token), err)
	}
	if got != key {
		t.Fatalf("cursor round trip = %+v, want %+v", got, key)
	}
}

// TestCursorWorstCaseKeyRoundTrip proves the decode cap is sized to the key
// fields: a key at every field bound still mints a token decodeCursor accepts.
func TestCursorWorstCaseKeyRoundTrip(t *testing.T) {
	t.Parallel()

	key := cursorKey{
		Version:      cursorVersion,
		RepoID:       strings.Repeat("r", maxCursorRepoIDLength),
		RelativePath: strings.Repeat("p", maxCursorPathLength),
		LineNumber:   1 << 30,
		Runtime:      strings.Repeat("t", maxCursorRuntimeLength),
		OrderIndex:   1 << 30,
	}
	token, err := encodeCursor(key)
	if err != nil {
		t.Fatalf("encodeCursor() error = %v", err)
	}
	if len(token) > maxCursorLength {
		t.Fatalf("worst-case token is %d chars, over the %d cap", len(token), maxCursorLength)
	}
	if got, err := decodeCursor(token); err != nil || got != key {
		t.Fatalf("worst-case round trip = %+v, %v; want %+v, nil", got, err, key)
	}
}

// TestCursorEncodeRefusesOverCap proves the encode guard: a key past the field
// bounds fails loudly at mint time instead of issuing a token the next call
// would reject mid-walk.
func TestCursorEncodeRefusesOverCap(t *testing.T) {
	t.Parallel()

	key := cursorKey{
		Version:      cursorVersion,
		RepoID:       "repository:r_a",
		RelativePath: strings.Repeat("d/", 4000),
		LineNumber:   1,
		Runtime:      "node",
		OrderIndex:   0,
	}
	if _, err := encodeCursor(key); err == nil {
		t.Fatal("encodeCursor minted a token past maxCursorLength")
	}
}
