// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

func TestSystemPromptCitesTheRuleAndKnownExamples(t *testing.T) {
	p := systemPrompt()
	for _, want := range []string{
		"naming.md",
		"rule 3",
		"workloadmaterialization", // already-fixed precedent, naming-remediation.md
		"iamcanassume",            // already-fixed precedent, naming-remediation.md
		"glued_compound",
		"acceptable",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("systemPrompt() missing %q", want)
		}
	}
}

func TestBuildUserPromptEmbedsEveryCandidate(t *testing.T) {
	candidates := []Candidate{
		{Path: "go/internal/reducer/workloadinstance", Name: "workloadinstance"},
		{Path: "go/internal/query/taghistory", Name: "taghistory"},
	}
	got := buildUserPrompt(candidates)
	for _, c := range candidates {
		if !strings.Contains(got, c.Path) {
			t.Errorf("buildUserPrompt() missing path %q in: %s", c.Path, got)
		}
		if !strings.Contains(got, c.Name) {
			t.Errorf("buildUserPrompt() missing name %q in: %s", c.Name, got)
		}
	}
}
