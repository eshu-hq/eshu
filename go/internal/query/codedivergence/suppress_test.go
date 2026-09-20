// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import "testing"

// TestSuppressGeneratedPinsPairAndNearMiss: a generated-file member is
// suppressed; the same name at a normal path is not.
func TestSuppressGeneratedPinsPairAndNearMiss(t *testing.T) {
	t.Parallel()

	suppressed := Member{EntityName: "helper", RelativePath: "gen/helper.pb.go", Language: "go"}
	if !SuppressGenerated(suppressed) {
		t.Fatal("generated member must suppress")
	}
	nearMiss := Member{EntityName: "helper", RelativePath: "gen/helper.go", Language: "go"}
	if SuppressGenerated(nearMiss) {
		t.Fatal("non-generated path must not suppress")
	}
}

// TestSuppressVendoredPinsPairAndNearMiss: a vendored member is suppressed;
// the same tree outside vendor is not.
func TestSuppressVendoredPinsPairAndNearMiss(t *testing.T) {
	t.Parallel()

	suppressed := Member{EntityName: "helper", RelativePath: "vendor/lib/help.go", Language: "go"}
	if !SuppressVendored(suppressed) {
		t.Fatal("vendored member must suppress")
	}
	nearMiss := Member{EntityName: "helper", RelativePath: "lib/help.go", Language: "go"}
	if SuppressVendored(nearMiss) {
		t.Fatal("non-vendored path must not suppress")
	}
}

// TestSuppressTestFilePinsPairAndNearMiss: test files suppress by default
// and survive only when the caller opts in with includeTests.
func TestSuppressTestFilePinsPairAndNearMiss(t *testing.T) {
	t.Parallel()

	suppressed := Member{EntityName: "helper", RelativePath: "a/helper_test.go", Language: "go"}
	if !SuppressTestFile(suppressed, false) {
		t.Fatal("test file must suppress by default")
	}
	if SuppressTestFile(suppressed, true) {
		t.Fatal("opted-in test file must survive")
	}
	nearMiss := Member{EntityName: "helper", RelativePath: "a/helper.go", Language: "go"}
	if SuppressTestFile(nearMiss, false) {
		t.Fatal("non-test file must not suppress")
	}
}

// TestSuppressTrivialAccessorPinsPairAndNearMiss: a small getter suppresses;
// the same getter shape grown past twice the floor does not.
func TestSuppressTrivialAccessorPinsPairAndNearMiss(t *testing.T) {
	t.Parallel()

	suppressed := Member{EntityName: "getName", RelativePath: "a/user.go", Language: "go", TokenCount: 60}
	if !SuppressTrivialAccessor(suppressed) {
		t.Fatal("small getter must suppress")
	}
	nearMiss := Member{EntityName: "getName", RelativePath: "a/user.go", Language: "go", TokenCount: 150}
	if SuppressTrivialAccessor(nearMiss) {
		t.Fatal("large getter must not suppress")
	}
	plain := Member{EntityName: "computeTotal", RelativePath: "a/user.go", Language: "go", TokenCount: 60}
	if SuppressTrivialAccessor(plain) {
		t.Fatal("non-accessor name must not suppress")
	}
}

// TestSuppressWrapperFamilyPinsPairAndNearMiss: five same-name copies across
// packages (the per-service wrapper class) suppress as a family; three
// same-name copies (a genuine small clone) do not.
func TestSuppressWrapperFamilyPinsPairAndNearMiss(t *testing.T) {
	t.Parallel()

	family := []Member{
		{EntityName: "recordAPICall", RelativePath: "svc/a/x.go"},
		{EntityName: "recordAPICall", RelativePath: "svc/b/x.go"},
		{EntityName: "recordAPICall", RelativePath: "svc/c/x.go"},
		{EntityName: "recordAPICall", RelativePath: "svc/d/x.go"},
		{EntityName: "recordAPICall", RelativePath: "svc/e/x.go"},
	}
	if !SuppressWrapperFamily(family) {
		t.Fatal("five-copy same-name family must suppress")
	}
	small := family[:3]
	if SuppressWrapperFamily(small) {
		t.Fatal("three-copy same-name group must not suppress")
	}
	mixed := []Member{
		{EntityName: "recordAPICall", RelativePath: "svc/a/x.go"},
		{EntityName: "otherName", RelativePath: "svc/b/x.go"},
		{EntityName: "recordAPICall", RelativePath: "svc/c/x.go"},
		{EntityName: "recordAPICall", RelativePath: "svc/d/x.go"},
		{EntityName: "recordAPICall", RelativePath: "svc/e/x.go"},
	}
	if SuppressWrapperFamily(mixed) {
		t.Fatal("mixed-name group must not suppress")
	}
}
