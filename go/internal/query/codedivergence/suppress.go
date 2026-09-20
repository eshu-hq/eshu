// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"path"
	"regexp"
	"strings"
)

// shippedPathConcern: every rule below reads only the member's path, name,
// language, and token count — signals the grouping SQL already returns. No
// rule reads a body, so suppression never touches source_cache.

var accessorNamePattern = regexp.MustCompile(`^(get|set|is|has|can)[A-Z0-9_]`)

// SuppressGenerated reports whether the member lives under a generated-file
// marker: a .pb.go protobuf output, a .generated. infix, or a generated path
// segment. A bare gen/ directory is not a marker (near-miss pinned by test).
func SuppressGenerated(member Member) bool {
	base := path.Base(member.RelativePath)
	if strings.HasSuffix(base, ".pb.go") {
		return true
	}
	if strings.Contains(base, ".generated.") {
		return true
	}
	for _, segment := range strings.Split(member.RelativePath, "/") {
		if segment == "generated" {
			return true
		}
	}
	return false
}

// SuppressVendored reports whether the member lives under a vendored tree:
// vendor, third_party, or node_modules. Everything else, including a
// top-level lib/ tree, is first-party.
func SuppressVendored(member Member) bool {
	for _, segment := range strings.Split(member.RelativePath, "/") {
		switch segment {
		case "vendor", "third_party", "node_modules":
			return true
		}
	}
	return false
}

// SuppressTestFile reports whether the member is a test file, using
// per-language test filename conventions: _test.go, test_*/​*_test.py,
// *.test/​*.spec.ts/js, *Test.java. Suppression is the default; passing
// includeTests opts the caller into seeing test copies.
func SuppressTestFile(member Member, includeTests bool) bool {
	if includeTests {
		return false
	}
	base := path.Base(member.RelativePath)
	switch {
	case strings.HasSuffix(base, "_test.go"):
		return true
	case strings.HasSuffix(base, "_test.py") || strings.HasPrefix(base, "test_"):
		return strings.HasSuffix(base, ".py")
	case strings.HasSuffix(base, ".test.ts"), strings.HasSuffix(base, ".test.js"),
		strings.HasSuffix(base, ".spec.ts"), strings.HasSuffix(base, ".spec.js"):
		return true
	case strings.HasSuffix(base, "Test.java"):
		return true
	}
	return false
}

// SuppressTrivialAccessor reports whether the member is a trivial accessor:
// a getter/setter/predicate-shaped name holding at most twice the token
// floor. Larger bodies under accessor names are real logic and keep
// reporting.
func SuppressTrivialAccessor(member Member) bool {
	if member.TokenCount > 2*TokenFloor {
		return false
	}
	return accessorNamePattern.MatchString(member.EntityName)
}

// SuppressWrapperFamily reports whether the whole surviving group is an
// intentional parallel family: at least WrapperFamilyMinMembers members
// sharing one non-empty entity name (the per-service wrapper class from
// #6834 §4). Smaller same-name groups are genuine small clones and keep
// reporting, as do mixed-name groups.
func SuppressWrapperFamily(members []Member) bool {
	if len(members) < WrapperFamilyMinMembers {
		return false
	}
	name := members[0].EntityName
	if name == "" {
		return false
	}
	for _, member := range members[1:] {
		if member.EntityName != name {
			return false
		}
	}
	return true
}
