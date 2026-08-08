// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestHardcodedSecretGoldenFixtureCandidateIsUnsuppressedAndRedacted(t *testing.T) {
	t.Parallel()

	const (
		runtimePath = "config/runtime.cfg"
		fixtureBody = "password = \"invalid-by-design\"\n"
	)
	fixturePath := filepath.Join(
		"..", "..", "..", "tests", "fixtures", "ecosystems", "deployable-config", filepath.FromSlash(runtimePath),
	)
	body, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read committed hardcoded-secret fixture: %v", err)
	}
	if got := string(body); got != fixtureBody {
		t.Fatalf("fixture body = %q, want %q", got, fixtureBody)
	}
	line := strings.TrimSuffix(string(body), "\n")

	detector := regexp.MustCompile("(?i)" + hardcodedSecretSQLPattern)
	if !detector.MatchString(line) {
		t.Fatalf("hardcoded-secret detector did not match %q", line)
	}
	passwordClassifier := regexp.MustCompile(`(?i)(password|passwd|pwd)[[:space:]]*[:=]`)
	if !passwordClassifier.MatchString(line) {
		t.Fatalf("password_literal classifier did not match %q", line)
	}

	if suppressions := hardcodedSecretSuppressions(runtimePath, line); len(suppressions) != 0 {
		t.Fatalf("hardcodedSecretSuppressions(%q) = %#v, want none", runtimePath, suppressions)
	}
	for _, forbidden := range []string{"example", "dummy", "placeholder", "changeme"} {
		if strings.Contains(strings.ToLower(line), forbidden) {
			t.Errorf("fixture line contains suppression marker %q", forbidden)
		}
	}
	for _, forbidden := range []string{"_test.", "/testdata/", "/fixtures/", "/examples/"} {
		if strings.Contains(strings.ToLower(runtimePath), forbidden) {
			t.Errorf("runtime path contains suppression marker %q", forbidden)
		}
	}

	confidence, severity := hardcodedSecretRisk("password_literal")
	if confidence != "medium" || severity != "high" {
		t.Fatalf("password_literal risk = confidence:%q severity:%q, want medium/high", confidence, severity)
	}
	redacted := redactHardcodedSecretLine(line)
	if got, want := redacted, `password = "[REDACTED]"`; got != want {
		t.Fatalf("redacted fixture line = %q, want %q", got, want)
	}
	if strings.Contains(redacted, "invalid-by-design") {
		t.Fatalf("redacted fixture line leaked the source literal: %q", redacted)
	}
}
