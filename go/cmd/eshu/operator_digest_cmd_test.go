// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestOperatorDigestCommandRejectsEmptyScope(t *testing.T) {
	cmd := newOperatorDigestCommand()
	cmd.SetArgs([]string{"--json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("command succeeded with empty scope, want error")
	}
	if !strings.Contains(err.Error(), "scope is required") {
		t.Fatalf("error = %v, want required scope error", err)
	}
}

func TestOperatorDigestCommandEmitsStableJSON(t *testing.T) {
	first := runOperatorDigestJSON(t, "--scope", "repo:demo/service-api", "--profile", "local_authoritative", "--json")
	second := runOperatorDigestJSON(t, "--scope", "repo:demo/service-api", "--profile", "local_authoritative", "--json")
	if !bytes.Equal(first, second) {
		t.Fatalf("operator digest output is not stable:\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	var digest operatorDigest
	if err := json.Unmarshal(first, &digest); err != nil {
		t.Fatalf("unmarshal digest JSON: %v\n%s", err, first)
	}
	if digest.Schema != operatorDigestSchema {
		t.Fatalf("schema = %q, want %q", digest.Schema, operatorDigestSchema)
	}
	if digest.Scope.Type != "repository" || digest.Scope.Label != "demo/service-api" {
		t.Fatalf("scope = %+v, want repository demo/service-api", digest.Scope)
	}
	if digest.Profile != "local_authoritative" {
		t.Fatalf("profile = %q, want local_authoritative", digest.Profile)
	}
	if digest.Truth.TruthClass != "unsupported" || digest.Truth.Freshness != "unavailable" {
		t.Fatalf("truth = %+v, want unsupported unavailable", digest.Truth)
	}
	if got, want := len(digest.Sections), len(operatorDigestSectionTemplates); got != want {
		t.Fatalf("sections = %d, want %d", got, want)
	}
	for i, section := range digest.Sections {
		if section.ID != operatorDigestSectionTemplates[i].ID {
			t.Fatalf("section[%d] id = %q, want %q", i, section.ID, operatorDigestSectionTemplates[i].ID)
		}
		if section.Status != "unsupported" {
			t.Fatalf("section[%d] status = %q, want unsupported", i, section.Status)
		}
		if len(section.Limitations) == 0 {
			t.Fatalf("section[%d] missing limitation", i)
		}
		if len(section.SourceRefs) == 0 {
			t.Fatalf("section[%d] missing source refs", i)
		}
	}
	if len(digest.SuggestedQuestions) == 0 {
		t.Fatal("suggested_questions is empty")
	}
	if digest.SuggestedQuestions[0].SourceSignal == "" {
		t.Fatalf("first suggested question missing source_signal: %+v", digest.SuggestedQuestions[0])
	}
	if digest.SuggestedQuestions[0].Why == "" {
		t.Fatalf("first suggested question missing why: %+v", digest.SuggestedQuestions[0])
	}
	if !strings.Contains(digest.SuggestedQuestions[0].Why, digest.SuggestedQuestions[0].SourceSignal) {
		t.Fatalf("question why %q does not reference source signal %q", digest.SuggestedQuestions[0].Why, digest.SuggestedQuestions[0].SourceSignal)
	}
	if len(digest.Limitations) == 0 {
		t.Fatal("digest limitations is empty")
	}
	if len(digest.SourceRefs) == 0 {
		t.Fatal("digest source_refs is empty")
	}
}

func TestOperatorDigestQuestionLimitTruncatesDeterministically(t *testing.T) {
	raw := runOperatorDigestJSON(t, "--scope", "service:payments-api", "--question-limit", "2", "--json")
	var digest operatorDigest
	if err := json.Unmarshal(raw, &digest); err != nil {
		t.Fatalf("unmarshal digest JSON: %v\n%s", err, raw)
	}
	if got := len(digest.SuggestedQuestions); got != 2 {
		t.Fatalf("suggested questions = %d, want 2", got)
	}
	if got, want := digest.SuggestedQuestions[0].ID, "operator_digest.v1:question:ambiguity_review_queue:service:payments-api"; got != want {
		t.Fatalf("first question id = %q, want %q", got, want)
	}
	if !operatorDigestHasLimitation(digest.Limitations, "suggested_questions_truncated") {
		t.Fatalf("digest limitations missing suggested_questions_truncated: %+v", digest.Limitations)
	}
}

func TestOperatorDigestTextRendersQuestionWhy(t *testing.T) {
	raw := runOperatorDigestText(t, "--scope", "repo:demo/service-api")
	for _, want := range []string{
		"Which missing or ambiguous evidence should be resolved before acting on this scope?",
		"why: unsupported section ambiguity_review_queue produced source signal",
		"operator_digest.v1:limitation:ambiguity_review_queue:repo:demo/service-api",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("operator digest text missing %q:\n%s", want, raw)
		}
	}
}

func TestOperatorDigestQuestionLimitRejectsContractOverflow(t *testing.T) {
	cmd := newOperatorDigestCommand()
	cmd.SetArgs([]string{"--scope", "repo:demo/service-api", "--question-limit", "26", "--json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("command succeeded with question limit above contract maximum, want error")
	}
	if !strings.Contains(err.Error(), "question-limit must be between 0 and 25") {
		t.Fatalf("error = %v, want question-limit bounds error", err)
	}
}

func TestOperatorDigestCommandRejectsUnsafeScope(t *testing.T) {
	cmd := newOperatorDigestCommand()
	cmd.SetArgs([]string{"--scope", "repo:https://example.invalid/private", "--json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("command succeeded with unsafe scope, want error")
	}
	if !strings.Contains(err.Error(), "scope must be share-safe") {
		t.Fatalf("error = %v, want share-safe scope error", err)
	}
}

func TestOperatorDigestCommandRejectsPrefixedAbsolutePathScope(t *testing.T) {
	cmd := newOperatorDigestCommand()
	cmd.SetArgs([]string{"--scope", "repo:/Users/example/private", "--json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("command succeeded with prefixed absolute path scope, want error")
	}
	if !strings.Contains(err.Error(), "scope must be share-safe") {
		t.Fatalf("error = %v, want share-safe scope error", err)
	}
}

func runOperatorDigestJSON(t *testing.T, args ...string) []byte {
	t.Helper()
	cmd := newOperatorDigestCommand()
	cmd.SetArgs(args)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("command returned error: %v\n%s", err, out.String())
	}
	return out.Bytes()
}

func runOperatorDigestText(t *testing.T, args ...string) string {
	t.Helper()
	cmd := newOperatorDigestCommand()
	cmd.SetArgs(args)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("command returned error: %v\n%s", err, out.String())
	}
	return out.String()
}

func operatorDigestHasLimitation(limitations []operatorDigestLimitation, reason string) bool {
	for _, limitation := range limitations {
		if limitation.Reason == reason {
			return true
		}
	}
	return false
}
