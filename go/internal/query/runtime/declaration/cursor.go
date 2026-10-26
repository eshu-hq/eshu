// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	// cursorVersion is the only cursor version this build issues or accepts. A
	// cursor with another version is rejected, and paging restarts: the route
	// has no side effects.
	cursorVersion = 1
	// maxCursorRepoIDLength, maxCursorPathLength and maxCursorRuntimeLength
	// bound the key string fields a cursor can carry. They mirror the request
	// filter caps (maxRepoIDLength, maxFilterLength) for repo_id and runtime;
	// relative_path comes from the graph, not the request, so it takes the
	// filesystem PATH_MAX instead of a filter cap.
	maxCursorRepoIDLength  = 512
	maxCursorPathLength    = 4096
	maxCursorRuntimeLength = 128
	// maxCursorLength bounds a cursor token before it is decoded. It is sized
	// to the key fields above: their JSON (4794 bytes at the field maxima)
	// encodes to a 6392-char token, so 8192 leaves headroom and every cursor
	// this route issues for realistic rows decodes. A pathological key (for
	// example thousands of JSON-escaped characters) trips the encode guard
	// instead and fails loudly at mint time; anything longer is otherwise not
	// a token this route issued.
	maxCursorLength = 8192
)

// errBadCursor is the one message every malformed cursor answers with.
var errBadCursor = errors.New("cursor is not a cursor this route issued; restart paging without it")

// cursorKey is the last row of a page, in the graph key order. The next page
// resumes strictly after it.
//
// The key is not sealed. It only positions the read: the caller's repository
// grant and every filter are applied before the cursor predicate, so a minted
// cursor cannot reach a row the caller could not already list.
type cursorKey struct {
	Version      int    `json:"v"`
	RepoID       string `json:"r"`
	RelativePath string `json:"p"`
	LineNumber   int    `json:"l"`
	Runtime      string `json:"t"`
	OrderIndex   int    `json:"o"`
}

// encodeCursor renders key as an opaque URL-safe token. It refuses to mint a
// token longer than maxCursorLength: decodeCursor would reject it on the next
// call and dead-end the page walk, so the failure surfaces here (a 500 with
// the step's fixed message) instead of stranding the caller mid-walk.
func encodeCursor(key cursorKey) (string, error) {
	raw, err := json.Marshal(key)
	if err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	if len(token) > maxCursorLength {
		return "", fmt.Errorf("cursor key of %d bytes exceeds the %d-character cursor bound", len(raw), maxCursorLength)
	}
	return token, nil
}

// decodeCursor parses a token from encodeCursor. It rejects other versions,
// negative positions, and keys with no repository.
func decodeCursor(token string) (cursorKey, error) {
	if len(token) > maxCursorLength {
		return cursorKey{}, errBadCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(token, "="))
	if err != nil {
		return cursorKey{}, errBadCursor
	}
	var key cursorKey
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&key); err != nil {
		return cursorKey{}, errBadCursor
	}
	if key.Version != cursorVersion || key.RepoID == "" || key.LineNumber < 0 || key.OrderIndex < 0 {
		return cursorKey{}, errBadCursor
	}
	return key, nil
}
