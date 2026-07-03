// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCopySearchIndexTermsToTableSkipsEmptyRows(t *testing.T) {
	t.Parallel()

	copied, err := (SQLDB{}).copySearchIndexTermsToTable(
		context.Background(),
		"eshu_search_index_terms",
		"scope-1",
		"gen-1",
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("copySearchIndexTermsToTable error = %v", err)
	}
	if copied != 0 {
		t.Fatalf("copied = %d, want 0", copied)
	}
}

func TestCopySearchIndexTermsToTableChecksAlignmentBeforeEmptySkip(t *testing.T) {
	t.Parallel()

	_, err := (SQLDB{}).copySearchIndexTermsToTable(
		context.Background(),
		"eshu_search_index_terms",
		"scope-1",
		"gen-1",
		[]string{"doc-1"},
		nil,
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("copySearchIndexTermsToTable error = nil, want alignment error")
	}
	if !strings.Contains(err.Error(), "requires aligned slices") {
		t.Fatalf("copySearchIndexTermsToTable error = %v, want aligned-slices message", err)
	}
}

func TestSearchIndexTermCopyUnsupportedErrorIsTyped(t *testing.T) {
	t.Parallel()

	err := searchIndexTermCopyUnsupportedError{driver: "testDriver"}
	var unsupported interface {
		UnsupportedSearchIndexTermCopy() bool
	}
	if !errors.As(err, &unsupported) {
		t.Fatal("searchIndexTermCopyUnsupportedError did not match unsupported interface")
	}
	if !unsupported.UnsupportedSearchIndexTermCopy() {
		t.Fatal("UnsupportedSearchIndexTermCopy() = false, want true")
	}
	if !strings.Contains(err.Error(), "testDriver") {
		t.Fatalf("error string %q missing driver", err.Error())
	}
}
