// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"regexp"
	"slices"
	"testing"
)

func TestCanonicalOwnedPackageEcosystem(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"gradle":    "maven",
		" Gradle ":  "maven",
		"maven":     "maven",
		"gomod":     "go",
		"go-module": "go",
		"go_module": "go",
		"go":        "go",
		"NPM":       "npm",
		"sbt":       "sbt",
		"":          "",
	}
	for raw, want := range cases {
		if got := CanonicalOwnedPackageEcosystem(raw); got != want {
			t.Errorf("CanonicalOwnedPackageEcosystem(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestOwnedPackageManagerAliases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		ecosystem string
		want      []string
	}{
		{"maven", []string{"gradle", "maven"}},
		{"gradle", []string{"gradle", "maven"}},
		{"go", []string{"go", "go-module", "go_module", "gomod"}},
		{"npm", []string{"npm"}},
		{"", nil},
	}
	for _, tc := range cases {
		if got := OwnedPackageManagerAliases(tc.ecosystem); !slices.Equal(got, tc.want) {
			t.Errorf("OwnedPackageManagerAliases(%q) = %#v, want %#v", tc.ecosystem, got, tc.want)
		}
	}
}

// TestOwnedPackageManagerAliasTableAgreesWithCanonicalMapping pins that the
// table the SQL CASE is built from and the Go mapping read the same data, and
// that every entry is safe to inline as a SQL string literal.
func TestOwnedPackageManagerAliasTableAgreesWithCanonicalMapping(t *testing.T) {
	t.Parallel()

	safeLiteral := regexp.MustCompile(`^[a-z0-9_-]+$`)
	table := OwnedPackageManagerAliasTable()
	if len(table) == 0 {
		t.Fatal("alias table is empty")
	}
	for _, entry := range table {
		if !safeLiteral.MatchString(entry.Alias) || !safeLiteral.MatchString(entry.Canonical) {
			t.Errorf("alias entry %+v is not a safe SQL literal", entry)
		}
		if got := CanonicalOwnedPackageEcosystem(entry.Alias); got != entry.Canonical {
			t.Errorf("CanonicalOwnedPackageEcosystem(%q) = %q, table says %q", entry.Alias, got, entry.Canonical)
		}
		if !slices.Contains(OwnedPackageManagerAliases(entry.Canonical), entry.Alias) {
			t.Errorf("OwnedPackageManagerAliases(%q) is missing alias %q", entry.Canonical, entry.Alias)
		}
	}
	if !slices.IsSortedFunc(table, func(a, b OwnedPackageManagerAlias) int {
		switch {
		case a.Alias < b.Alias:
			return -1
		case a.Alias > b.Alias:
			return 1
		}
		return 0
	}) {
		t.Error("alias table is not sorted, so generated SQL would not be byte-stable")
	}
}
