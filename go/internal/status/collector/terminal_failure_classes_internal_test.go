// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"os"
	"regexp"
	"testing"
)

// TestKindFailureClassesNameFirstPartyKinds pins that every key of the
// per-kind map is a collector kind the platform ships, so a misspelled kind
// cannot leave its classes unreachable while the tests for other kinds pass.
func TestKindFailureClassesNameFirstPartyKinds(t *testing.T) {
	t.Parallel()
	for kind, classes := range kindFailureClasses {
		if _, ok := firstPartyKinds[kind]; !ok {
			t.Errorf("kindFailureClasses key %q is not a first-party collector kind", kind)
		}
		if len(classes) == 0 {
			t.Errorf("kindFailureClasses[%q] is empty, want classes or no entry", kind)
		}
	}
}

// TestExtensionFailureClassPatternMatchesTheHost pins that the shape the status
// leaf uses to collapse an extension-defined class equals the shape the
// extension host enforces. The host package is read as source so the status
// leaf stays free of collector runtime imports in shipped code.
func TestExtensionFailureClassPatternMatchesTheHost(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("../../collector/extensionhost/failure.go")
	if err != nil {
		t.Fatalf("read extension host failure.go: %v", err)
	}
	m := regexp.MustCompile("failureClassPattern\\s*=\\s*regexp\\.MustCompile\\(`([^`]+)`\\)").FindSubmatch(src)
	if m == nil {
		t.Fatal("extensionhost failureClassPattern declaration not found; update this test with the host")
	}
	if got, want := extensionFailureClassPattern.String(), string(m[1]); got != want {
		t.Fatalf("status pattern %q differs from the extension host pattern %q", got, want)
	}
}
