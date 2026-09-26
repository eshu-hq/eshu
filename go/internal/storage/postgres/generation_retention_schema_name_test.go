// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"regexp"
	"strings"
	"testing"
)

var generationRetentionSchemaIdentifier = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

func TestGenerationRetentionMigratedSchemaNameIsAValidBoundedIdentifier(t *testing.T) {
	t.Parallel()
	for _, testName := range []string{
		"TestGenerationRetentionPrunesMigratedSchemaLive",
		"TestGenerationRetentionStatementsPrepareAgainstMigratedSchemaLive/prune_content_file_references",
		"Test-With Spaces/and.Dots/andéunicode",
		"",
		strings.Repeat("TestVeryLong", 40),
	} {
		got := generationRetentionMigratedSchemaName(testName)
		if !generationRetentionSchemaIdentifier.MatchString(got) {
			t.Errorf("name(%q) = %q, want an unquoted lowercase identifier", testName, got)
		}
		if len(got) > generationRetentionSchemaMaxBytes {
			t.Errorf("name(%q) is %d bytes, want <= %d", testName, len(got), generationRetentionSchemaMaxBytes)
		}
		if !strings.HasPrefix(got, generationRetentionSchemaPrefix) {
			t.Errorf("name(%q) = %q, want prefix %q", testName, got, generationRetentionSchemaPrefix)
		}
	}
}

func TestGenerationRetentionMigratedSchemaNameSanitizesSubtestSlashes(t *testing.T) {
	t.Parallel()
	got := generationRetentionMigratedSchemaName("TestA/Sub Case/x")
	if want := "eshu_ret_testa_sub_case_x"; got != want {
		t.Errorf("name = %q, want %q", got, want)
	}
}

func TestGenerationRetentionMigratedSchemaNameIsDeterministic(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("TestDeterministic", 10)
	if a, b := generationRetentionMigratedSchemaName(long), generationRetentionMigratedSchemaName(long); a != b {
		t.Errorf("long name not deterministic: %q != %q", a, b)
	}
	if a, b := generationRetentionMigratedSchemaName("TestShort"), generationRetentionMigratedSchemaName("TestShort"); a != b {
		t.Errorf("short name not deterministic: %q != %q", a, b)
	}
}

func TestGenerationRetentionMigratedSchemaNameKeepsLongNamesDistinct(t *testing.T) {
	t.Parallel()
	shared := strings.Repeat("TestGenerationRetentionSharedPrefix/", 4)
	a := generationRetentionMigratedSchemaName(shared + "case_a")
	b := generationRetentionMigratedSchemaName(shared + "case_b")
	if a == b {
		t.Fatalf("long names differing only at the end collide: %q", a)
	}
	if len(a) > generationRetentionSchemaMaxBytes || len(b) > generationRetentionSchemaMaxBytes {
		t.Fatalf("names exceed %d bytes: %d, %d", generationRetentionSchemaMaxBytes, len(a), len(b))
	}
	// Names that sanitize identically but differ in the original (case, punctuation)
	// and overflow must still be told apart by the hash of the original name.
	c := generationRetentionMigratedSchemaName(strings.Repeat("A-", 40))
	d := generationRetentionMigratedSchemaName(strings.Repeat("a_", 40))
	if c == d {
		t.Errorf("distinct long originals with one sanitized form collide: %q", c)
	}
}
