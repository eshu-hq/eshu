// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

const (
	// cursorVersion is the only cursor version this build issues or accepts. A
	// cursor with another version is rejected, and paging restarts: the route
	// has no side effects.
	cursorVersion = 1
	// maxCursorLength bounds an incoming cursor before it is decoded.
	maxCursorLength = 2048
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

// encodeCursor renders key as an opaque URL-safe token.
func encodeCursor(key cursorKey) (string, error) {
	raw, err := json.Marshal(key)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
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
