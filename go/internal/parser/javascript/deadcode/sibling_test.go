// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/javascript/project"
)

// TestNilSiblingSourceAnswersAbsentRatherThanPanicking pins the nil tolerance
// SiblingSource documents.
//
// Before issue #6771 the sibling parser was passed as a concrete
// *javaScriptSiblingParser, and its rootForFile guarded p == nil, so a nil
// parser answered "no sibling evidence" and callers treated that as absence.
// Replacing the parameter with an interface silently changed that: a method
// call on a nil INTERFACE panics, where a nil typed pointer does not. The
// existing walk-count tests pass a literal nil and did not catch it, because
// they also pass repoRoot "" and return before the seam is ever consulted.
//
// This test reaches the seam deliberately. The control below fails loudly if a
// guard earlier in javaScriptIsHapiHandlerFile starts short-circuiting first,
// because a probe that returns early would pass while proving nothing.
func TestNilSiblingSourceAnswersAbsentRatherThanPanicking(t *testing.T) {
	const (
		repoRoot = "/tmp/eshu-nil-sibling-probe"
		// A "/handlers/" segment in the REPO-RELATIVE path needs a directory
		// above handlers/, or the Contains check returns before the seam.
		path = "/tmp/eshu-nil-sibling-probe/src/handlers/route.js"
	)

	relative, ok := project.RelativeSlashPath(repoRoot, path)
	if !ok || !strings.Contains(relative, "/handlers/") {
		t.Fatalf(
			"control failed, this test would prove nothing: RelativeSlashPath(%q, %q) = %q, %v; want a path containing \"/handlers/\"",
			repoRoot, path, relative, ok,
		)
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("nil SiblingSource panicked instead of answering absent: %v", recovered)
		}
	}()

	if got := javaScriptIsHapiHandlerFile(repoRoot, path, nil); got {
		t.Errorf("javaScriptIsHapiHandlerFile with a nil SiblingSource = true, want false")
	}
}
