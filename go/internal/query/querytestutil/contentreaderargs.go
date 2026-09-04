// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"bytes"
	"database/sql/driver"
	"fmt"
	"strings"
)

// ContentReaderQueryContainsInOrder reports whether query contains every
// fragment, each one after the previous.
//
// Matching advances past the fragment it consumed, so a repeated fragment has
// to appear as many times as it is listed rather than matching its own earlier
// occurrence. That ordering is what distinguishes a clause emitted in the wrong
// position from one merely present, which a plain contains check cannot see.
//
// It is exported for tests that hold a recorded query string rather than a
// queued result, and it is the same check ContentReaderQueryResult's
// QueryContainsInOrder runs.
func ContentReaderQueryContainsInOrder(query string, fragments []string) error {
	offset := 0
	for _, fragment := range fragments {
		index := strings.Index(query[offset:], fragment)
		if index < 0 {
			return fmt.Errorf("query missing ordered fragment %q", fragment)
		}
		offset += index + len(fragment)
	}
	return nil
}

// ContentReaderCheckArgs asserts args carries exactly the values in want, in
// the same $1, $2, ... order. A nil want skips the check -- most fake-driver
// tests only assert query text, and want stays nil for them. args is already
// past driver.DefaultParameterConverter (a pgarray.Array argument arrives here
// as the array's already-converted literal string, not the original slice), so
// want must hold the converted form too.
func ContentReaderCheckArgs(args []driver.NamedValue, want []driver.Value) error {
	if want == nil {
		return nil
	}
	if len(args) != len(want) {
		return fmt.Errorf("query got %d bind args, want %d: %#v", len(args), len(want), args)
	}
	for i, wantValue := range want {
		if !contentReaderArgEqual(args[i].Value, wantValue) {
			return fmt.Errorf("query bind arg $%d = %#v (%T), want %#v (%T)",
				i+1, args[i].Value, args[i].Value, wantValue, wantValue)
		}
	}
	return nil
}

// contentReaderArgEqual compares a driver-converted bind value against an
// expected value, tolerating int vs int64: a want value written as a plain Go
// int still matches the int64 driver.DefaultParameterConverter actually
// produces. A []byte bind value (package query binds JSONB parameters as
// []byte) is compared with bytes.Equal before the plain `==` fallthrough: Go
// panics comparing two interface values holding the same uncomparable dynamic
// type, and []byte is uncomparable, so `got == want` on two []byte args panics
// instead of reporting a mismatch (#5764 round-9 P3-3 review follow-up).
func contentReaderArgEqual(got, want driver.Value) bool {
	if gotInt, ok := contentReaderAsInt64(got); ok {
		if wantInt, ok := contentReaderAsInt64(want); ok {
			return gotInt == wantInt
		}
	}
	if gotBytes, ok := got.([]byte); ok {
		wantBytes, ok := want.([]byte)
		if !ok {
			return false
		}
		return bytes.Equal(gotBytes, wantBytes)
	}
	return got == want
}

// contentReaderAsInt64 widens the two integer shapes a bind expectation is
// written in, and reports false for anything else so a non-numeric comparison
// falls through unchanged.
func contentReaderAsInt64(v driver.Value) (int64, bool) {
	switch typed := v.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	default:
		return 0, false
	}
}
