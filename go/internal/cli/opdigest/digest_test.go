// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package opdigest

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestOptionsFromFlagsRejectsEmptyScope(t *testing.T) {
	_, err := OptionsFromFlags("", DefaultProfile, DefaultQuestionMax)
	if err == nil {
		t.Fatal("OptionsFromFlags succeeded with empty scope, want error")
	}
	if !strings.Contains(err.Error(), "scope is required") {
		t.Fatalf("error = %v, want required scope error", err)
	}
}

func TestOptionsFromFlagsRejectsUnsafeScope(t *testing.T) {
	_, err := OptionsFromFlags("repo:https://example.invalid/private", DefaultProfile, DefaultQuestionMax)
	if err == nil {
		t.Fatal("OptionsFromFlags succeeded with unsafe scope, want error")
	}
	if !strings.Contains(err.Error(), "scope must be share-safe") {
		t.Fatalf("error = %v, want share-safe scope error", err)
	}
}

func TestOptionsFromFlagsRejectsPrefixedAbsolutePathScope(t *testing.T) {
	_, err := OptionsFromFlags("repo:/Users/example/private", DefaultProfile, DefaultQuestionMax)
	if err == nil {
		t.Fatal("OptionsFromFlags succeeded with prefixed absolute path scope, want error")
	}
	if !strings.Contains(err.Error(), "scope must be share-safe") {
		t.Fatalf("error = %v, want share-safe scope error", err)
	}
}

func TestOptionsFromFlagsRejectsQuestionLimitAboveContractMax(t *testing.T) {
	_, err := OptionsFromFlags("repo:demo/service-api", DefaultProfile, 26)
	if err == nil {
		t.Fatal("OptionsFromFlags succeeded with question limit above contract maximum, want error")
	}
	if !strings.Contains(err.Error(), "question-limit must be between 0 and 25") {
		t.Fatalf("error = %v, want question-limit bounds error", err)
	}
}

func TestBuildDigestIsStableAndWellFormed(t *testing.T) {
	options, err := OptionsFromFlags("repo:demo/service-api", "local_authoritative", DefaultQuestionMax)
	if err != nil {
		t.Fatalf("OptionsFromFlags: %v", err)
	}

	first, err := json.Marshal(BuildDigest(options))
	if err != nil {
		t.Fatalf("marshal first digest: %v", err)
	}
	second, err := json.Marshal(BuildDigest(options))
	if err != nil {
		t.Fatalf("marshal second digest: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("operator digest output is not stable:\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	digest := BuildDigest(options)
	if digest.Schema != Schema {
		t.Fatalf("schema = %q, want %q", digest.Schema, Schema)
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
	if got, want := len(digest.Sections), len(sectionTemplates); got != want {
		t.Fatalf("sections = %d, want %d", got, want)
	}
	for i, section := range digest.Sections {
		if section.ID != sectionTemplates[i].ID {
			t.Fatalf("section[%d] id = %q, want %q", i, section.ID, sectionTemplates[i].ID)
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

func TestBuildDigestQuestionLimitTruncatesDeterministically(t *testing.T) {
	options, err := OptionsFromFlags("service:payments-api", DefaultProfile, 2)
	if err != nil {
		t.Fatalf("OptionsFromFlags: %v", err)
	}
	digest := BuildDigest(options)
	if got := len(digest.SuggestedQuestions); got != 2 {
		t.Fatalf("suggested questions = %d, want 2", got)
	}
	if got, want := digest.SuggestedQuestions[0].ID, "operator_digest.v1:question:ambiguity_review_queue:service:payments-api"; got != want {
		t.Fatalf("first question id = %q, want %q", got, want)
	}
	if !hasLimitation(digest.Limitations, "suggested_questions_truncated") {
		t.Fatalf("digest limitations missing suggested_questions_truncated: %+v", digest.Limitations)
	}
}

func TestBuildDigestAllowsZeroQuestionLimit(t *testing.T) {
	options, err := OptionsFromFlags("repo:demo/service-api", DefaultProfile, 0)
	if err != nil {
		t.Fatalf("OptionsFromFlags: %v", err)
	}
	digest := BuildDigest(options)
	if len(digest.SuggestedQuestions) != 0 {
		t.Fatalf("suggested questions = %d, want 0", len(digest.SuggestedQuestions))
	}
}

func TestRenderTextRendersQuestionWhy(t *testing.T) {
	options, err := OptionsFromFlags("repo:demo/service-api", DefaultProfile, DefaultQuestionMax)
	if err != nil {
		t.Fatalf("OptionsFromFlags: %v", err)
	}
	var buf bytes.Buffer
	RenderText(&buf, BuildDigest(options))
	raw := buf.String()
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

func hasLimitation(limitations []Limitation, reason string) bool {
	for _, limitation := range limitations {
		if limitation.Reason == reason {
			return true
		}
	}
	return false
}
