// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package db

import (
	"reflect"
	"testing"
)

func TestCleanIDsTrimsDedupesAndDropsBlanks(t *testing.T) {
	t.Parallel()

	got := CleanIDs([]string{" b ", "a", "b", "", "  ", "a", "c"})
	want := []string{"b", "a", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CleanIDs = %#v, want %#v", got, want)
	}
	if got := CleanIDs(nil); len(got) != 0 {
		t.Fatalf("CleanIDs(nil) = %#v, want empty", got)
	}
	if got := CleanIDs([]string{"", "   "}); len(got) != 0 {
		t.Fatalf("CleanIDs(blanks) = %#v, want empty", got)
	}
}

func TestIDPlaceholdersRendersPositionalParams(t *testing.T) {
	t.Parallel()

	if got, want := IDPlaceholders(3), "$1, $2, $3"; got != want {
		t.Fatalf("IDPlaceholders(3) = %q, want %q", got, want)
	}
	if got, want := IDPlaceholders(1), "$1"; got != want {
		t.Fatalf("IDPlaceholders(1) = %q, want %q", got, want)
	}
	if got := IDPlaceholders(0); got != "" {
		t.Fatalf("IDPlaceholders(0) = %q, want empty", got)
	}
}

func TestIDArgsBindsIDsBeforeExtras(t *testing.T) {
	t.Parallel()

	got := IDArgs([]string{"a", "b"}, "extra", 7)
	want := []any{"a", "b", "extra", 7}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IDArgs = %#v, want %#v", got, want)
	}
	if got := IDArgs(nil); len(got) != 0 {
		t.Fatalf("IDArgs(nil) = %#v, want empty", got)
	}
}
