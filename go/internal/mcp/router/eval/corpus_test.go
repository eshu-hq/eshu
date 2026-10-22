// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package eval

import (
	"strings"
	"testing"
)

// smallRequirements relaxes the size floors so unit cases can use a corpus of a
// few questions while still exercising every other rule.
func smallRequirements() Requirements {
	return Requirements{MinQuestions: 1, MinDistinctTools: 1}
}

func validQuestion() Question {
	return Question{
		ID:         "code-lookup-001",
		Family:     "code_lookup",
		Question:   "Where is process_payment defined",
		Expected:   "find_symbol",
		Alternates: []string{"find_code"},
		Source:     "hand_written",
	}
}

func registeredSet() []string { return []string{"find_symbol", "find_code", "get_repo_story"} }

func TestLoadEmbeddedCorpus(t *testing.T) {
	t.Parallel()
	c, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	if got := len(c.Questions); got < 150 {
		t.Fatalf("embedded corpus has %d questions, want at least 150", got)
	}
}

func TestLoadRejectsMalformedInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		doc  string
	}{
		{"not json", `{`},
		{"unknown top-level field", `{"version":1,"questions":[],"extra":true}`},
		{"unknown question field", `{"version":1,"questions":[{"id":"a","bogus":1}]}`},
		{"unknown anchor field", `{"version":1,"questions":[{"id":"a","anchors":{"tenant":"x"}}]}`},
		{"trailing data", `{"version":1,"questions":[]} {}`},
		{"wrong version", `{"version":2,"questions":[]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Load(strings.NewReader(tc.doc)); err == nil {
				t.Fatalf("Load(%q) succeeded, want an error", tc.doc)
			}
		})
	}
}

func TestValidateAcceptsWellFormedCorpus(t *testing.T) {
	t.Parallel()
	c := &Corpus{Version: SchemaVersion, Questions: []Question{validQuestion()}}
	if err := Validate(c, registeredSet(), smallRequirements()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// TestValidateRejectsSeededViolations is the RED/GREEN pair for the schema
// gate: each case plants one violation in an otherwise valid question and the
// error must name it.
func TestValidateRejectsSeededViolations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(q *Question)
		want   string
	}{
		{"unregistered expected tool", func(q *Question) { q.Expected = "no_such_tool" }, "no_such_tool"},
		{"unregistered alternate", func(q *Question) { q.Alternates = []string{"ghost_tool"} }, "ghost_tool"},
		{"alternate repeats expected", func(q *Question) { q.Alternates = []string{q.Expected} }, "repeats the expected"},
		{"duplicate alternate", func(q *Question) { q.Alternates = []string{"find_code", "find_code"} }, "duplicate alternate"},
		{"empty id", func(q *Question) { q.ID = "" }, "empty id"},
		{"blank question", func(q *Question) { q.Question = "   " }, "empty question"},
		{"unknown family", func(q *Question) { q.Family = "misc" }, `unknown family "misc"`},
		{"unknown source", func(q *Question) { q.Source = "vibes" }, `unknown source "vibes"`},
		{"empty expected", func(q *Question) { q.Expected = "" }, "empty expected"},
		{"router tool as expected", func(q *Question) { q.Expected = "find_tool" }, "router"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			q := validQuestion()
			tc.mutate(&q)
			c := &Corpus{Version: SchemaVersion, Questions: []Question{q}}
			err := Validate(c, append(registeredSet(), "find_tool"), smallRequirements())
			if err == nil {
				t.Fatal("Validate succeeded, want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestValidateRejectsDuplicates(t *testing.T) {
	t.Parallel()
	a := validQuestion()
	b := validQuestion()
	b.Question = "something else entirely"
	c := &Corpus{Version: SchemaVersion, Questions: []Question{a, b}}
	if err := Validate(c, registeredSet(), smallRequirements()); err == nil || !strings.Contains(err.Error(), "duplicate id") {
		t.Fatalf("duplicate id: err = %v", err)
	}
	b.ID = "code-lookup-002"
	b.Question = "  WHERE is process_payment defined "
	c = &Corpus{Version: SchemaVersion, Questions: []Question{a, b}}
	if err := Validate(c, registeredSet(), smallRequirements()); err == nil || !strings.Contains(err.Error(), "duplicate question") {
		t.Fatalf("duplicate question: err = %v", err)
	}
}

func TestValidateEnforcesRequirements(t *testing.T) {
	t.Parallel()
	c := &Corpus{Version: SchemaVersion, Questions: []Question{validQuestion()}}
	tests := []struct {
		name string
		req  Requirements
		want string
	}{
		{"too few questions", Requirements{MinQuestions: 2}, "at least 2 questions"},
		{"too few tools", Requirements{MinDistinctTools: 2}, "at least 2 distinct"},
		{"missing family", Requirements{RequiredFamilies: []string{"dead_code"}}, `family "dead_code"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := Validate(c, registeredSet(), tc.req)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want mention of %q", err, tc.want)
			}
		})
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	t.Parallel()
	q1 := validQuestion()
	q1.Expected = "ghost_one"
	q2 := validQuestion()
	q2.ID = "code-lookup-002"
	q2.Question = "second question"
	q2.Expected = "ghost_two"
	c := &Corpus{Version: SchemaVersion, Questions: []Question{q1, q2}}
	err := Validate(c, registeredSet(), smallRequirements())
	if err == nil || !strings.Contains(err.Error(), "ghost_one") || !strings.Contains(err.Error(), "ghost_two") {
		t.Fatalf("err = %v, want both unregistered tools named", err)
	}
}

func TestDefaultRequirementsCoverEpicFamilies(t *testing.T) {
	t.Parallel()
	req := DefaultRequirements()
	if req.MinQuestions != 150 || req.MinDistinctTools != 40 {
		t.Fatalf("floors = %d questions, %d tools, want 150 and 40", req.MinQuestions, req.MinDistinctTools)
	}
	want := []string{"code_lookup", "change_impact", "supply_chain", "drift_replatforming", "service_workload", "dead_code"}
	for _, f := range want {
		found := false
		for _, got := range req.RequiredFamilies {
			found = found || got == f
		}
		if !found {
			t.Errorf("DefaultRequirements lacks epic family %q", f)
		}
	}
}
