// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package selector

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// TestExactMatchesSharedVersionTable pins Exact to the table every Gradle
// reader shares.
func TestExactMatchesSharedVersionTable(t *testing.T) {
	t.Parallel()
	file, err := os.Open("testdata/versions.txt")
	if err != nil {
		t.Fatalf("open table: %v", err)
	}
	defer func() { _ = file.Close() }()
	rows := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		want, text, ok := strings.Cut(line, "\t")
		if !ok || (want != "exact" && want != "not-exact") {
			t.Fatalf("malformed table line %q", line)
		}
		rows++
		if got := Exact(text); got != (want == "exact") {
			t.Errorf("Exact(%q) = %v, want %s", text, got, want)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read table: %v", err)
	}
	if rows == 0 {
		t.Fatal("table is empty")
	}
}

// The character floor rejects text no Gradle notation can carry as a plain
// version: empty text, whitespace, quotes, escapes, interpolation and shell
// constructs, control bytes, and non-ASCII.
func TestExactRejectsTextBelowTheCharacterFloor(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		"", " ", "1 0", "1.0 ", "\t1.0", "1'0", "1\"0", "1\\0", "$v", "${v}",
		"1`0", "1{0", "1}0", "1<0", "1>0", "1|0", "1;0", "1&0", "1\x000", "1\x7f",
		"1.0-ß", "é", "latest.", "LATEST.", "latest.x", "1.0:jdk8", ":",
	} {
		if Exact(text) {
			t.Errorf("Exact(%q) = true, want false", text)
		}
	}
	for _, text := range []string{"1", "lates", "latest", "latest-release", "latestx.y", "1.0-r#1"} {
		if !Exact(text) {
			t.Errorf("Exact(%q) = false, want true", text)
		}
	}
}

// Exact allocates nothing: it sits on the parse path of every Gradle row.
func TestExactAllocatesNothing(t *testing.T) {
	allocs := testing.AllocsPerRun(1000, func() {
		_ = Exact("1.2.3-SNAPSHOT")
		_ = Exact("Latest.Release")
		_ = Exact("[1.0,2.0)")
	})
	if allocs != 0 {
		t.Fatalf("Exact allocs per run = %v, want 0", allocs)
	}
}
