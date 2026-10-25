// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scope

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"testing"
)

func TestNewSecurityAlertTargetSetSortsAndDeduplicates(t *testing.T) {
	t.Parallel()

	set := NewSecurityAlertTargetSet([]string{"acme/b", "acme/a", "acme/b", " ", ""})
	if got, want := set.Slugs(), []string{"acme/a", "acme/b"}; !slices.Equal(got, want) {
		t.Fatalf("Slugs() = %v, want %v", got, want)
	}
	if set.IsZero() {
		t.Fatal("IsZero() = true for a recorded set")
	}
	if got, want := set.JSON(), `["acme/a","acme/b"]`; got != want {
		t.Fatalf("JSON() = %q, want %q", got, want)
	}
}

func TestSecurityAlertTargetSetHashIsTheSHA256OfTheCanonicalArray(t *testing.T) {
	t.Parallel()

	sum := sha256.Sum256([]byte(`["acme/a","acme/b"]`))
	want := "sha256:" + hex.EncodeToString(sum[:])
	if got := NewSecurityAlertTargetSet([]string{"acme/b", "acme/a"}).Hash(); got != want {
		t.Fatalf("Hash() = %q, want %q", got, want)
	}
	// Order and duplicates never move the hash; membership does.
	if a, b := NewSecurityAlertTargetSet([]string{"acme/a", "acme/b"}).Hash(),
		NewSecurityAlertTargetSet([]string{"acme/b", "acme/a", "acme/a"}).Hash(); a != b {
		t.Fatalf("hash depends on order: %q != %q", a, b)
	}
	if a, b := NewSecurityAlertTargetSet([]string{"acme/a"}).Hash(),
		NewSecurityAlertTargetSet([]string{"acme/a", "acme/c"}).Hash(); a == b {
		t.Fatal("adding a repository did not move the hash")
	}
}

func TestSecurityAlertTargetSetZeroValueRecordsNothing(t *testing.T) {
	t.Parallel()

	var unset SecurityAlertTargetSet
	if !unset.IsZero() {
		t.Fatal("zero value IsZero() = false")
	}
	if unset.Hash() != "" || unset.JSON() != "" || unset.Slugs() != nil {
		t.Fatalf("zero value records a set: hash=%q json=%q slugs=%v", unset.Hash(), unset.JSON(), unset.Slugs())
	}
	// An empty recorded set is still a recorded set: it covers no repository.
	empty := NewSecurityAlertTargetSet(nil)
	if empty.IsZero() || empty.JSON() != "[]" {
		t.Fatalf("empty set: IsZero=%v JSON=%q", empty.IsZero(), empty.JSON())
	}
}

func TestScopeGenerationStaysComparableWithTargetSet(t *testing.T) {
	t.Parallel()

	a := ScopeGeneration{GenerationID: "g", SecurityAlertTargetSet: NewSecurityAlertTargetSet([]string{"acme/a"})}
	b := ScopeGeneration{GenerationID: "g", SecurityAlertTargetSet: NewSecurityAlertTargetSet([]string{"acme/a"})}
	if a != b {
		t.Fatal("equal generations compare unequal")
	}
	b.SecurityAlertTargetSet = NewSecurityAlertTargetSet([]string{"acme/b"})
	if a == b {
		t.Fatal("generations with different target sets compare equal")
	}
}
