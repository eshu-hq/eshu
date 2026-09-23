// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package db

import (
	"errors"
	"strings"
	"testing"
)

func TestSearchIndexTermCopyUnsupportedErrorIsTyped(t *testing.T) {
	t.Parallel()

	err := SearchIndexTermCopyUnsupportedError{Driver: "testDriver"}
	var unsupported interface {
		UnsupportedSearchIndexTermCopy() bool
	}
	if !errors.As(err, &unsupported) {
		t.Fatal("SearchIndexTermCopyUnsupportedError did not match unsupported interface")
	}
	if !unsupported.UnsupportedSearchIndexTermCopy() {
		t.Fatal("UnsupportedSearchIndexTermCopy() = false, want true")
	}
	if !strings.Contains(err.Error(), "testDriver") {
		t.Fatalf("error string %q missing driver", err.Error())
	}
}

func TestSearchIndexTermCopyUnsupportedErrorBlankDriver(t *testing.T) {
	t.Parallel()

	err := SearchIndexTermCopyUnsupportedError{}
	if got, want := err.Error(), "search-index term copy is unsupported by this database"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}
