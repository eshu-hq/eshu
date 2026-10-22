// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package eval

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Requirements are the size and coverage floors a corpus must meet.
type Requirements struct {
	// MinQuestions is the least number of questions.
	MinQuestions int
	// MinDistinctTools is the least number of distinct expected tools.
	MinDistinctTools int
	// RequiredFamilies must each hold at least one question.
	RequiredFamilies []string
}

// DefaultRequirements returns the floors from issue #8756: 150 questions, 40
// distinct tools, and every family in the epic's table.
func DefaultRequirements() Requirements {
	return Requirements{
		MinQuestions:     150,
		MinDistinctTools: 40,
		RequiredFamilies: []string{
			FamilyCodeLookup, FamilyChangeImpact, FamilySupplyChain,
			FamilyDriftReplatforming, FamilyServiceWorkload, FamilyDeadCode,
		},
	}
}

// Validate checks the corpus shape against the registered tool names and the
// size floors in req. It reports every problem it finds, joined, so one run
// shows the whole repair list. A nil error means the corpus is usable; accuracy
// is never part of validation.
func Validate(c *Corpus, registered []string, req Requirements) error {
	if c == nil {
		return errors.New("corpus is nil")
	}
	known := make(map[string]bool, len(registered))
	for _, name := range registered {
		known[name] = true
	}
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	ids := map[string]bool{}
	texts := map[string]string{}
	tools := map[string]bool{}
	families := map[string]bool{}
	for i, q := range c.Questions {
		label := q.ID
		if label == "" {
			label = fmt.Sprintf("question #%d", i+1)
			fail("%s: empty id", label)
		} else if ids[q.ID] {
			fail("%s: duplicate id", label)
		}
		ids[q.ID] = true

		text := strings.ToLower(strings.Join(strings.Fields(q.Question), " "))
		switch {
		case text == "":
			fail("%s: empty question", label)
		case texts[text] != "":
			fail("%s: duplicate question text, same as %s", label, texts[text])
		default:
			texts[text] = label
		}

		if !knownFamilies[q.Family] {
			fail("%s: unknown family %q", label, q.Family)
		}
		families[q.Family] = true
		if !knownSources[q.Source] {
			fail("%s: unknown source %q", label, q.Source)
		}
		validateTools(label, q, known, fail)
		if q.Expected != "" {
			tools[q.Expected] = true
		}
	}

	if len(c.Questions) < req.MinQuestions {
		fail("corpus has %d questions, want at least %d questions", len(c.Questions), req.MinQuestions)
	}
	if len(tools) < req.MinDistinctTools {
		fail("corpus expects %d tools, want at least %d distinct tools", len(tools), req.MinDistinctTools)
	}
	required := append([]string(nil), req.RequiredFamilies...)
	sort.Strings(required)
	for _, f := range required {
		if !families[f] {
			fail("no question covers required family %q", f)
		}
	}
	return errors.Join(errs...)
}

// validateTools checks the expected tool and alternates of one question.
func validateTools(label string, q Question, known map[string]bool, fail func(string, ...any)) {
	checkTool := func(role, name string) {
		switch {
		case routerOnlyTools[name]:
			fail("%s: %s %q is a router surface, not a routable target", label, role, name)
		case !known[name]:
			fail("%s: %s tool %q is not registered", label, role, name)
		}
	}
	if q.Expected == "" {
		fail("%s: empty expected tool", label)
	} else {
		checkTool("expected", q.Expected)
	}
	seen := map[string]bool{}
	for _, alt := range q.Alternates {
		switch {
		case alt == q.Expected:
			fail("%s: alternate %q repeats the expected tool", label, alt)
		case seen[alt]:
			fail("%s: duplicate alternate %q", label, alt)
		default:
			checkTool("alternate", alt)
		}
		seen[alt] = true
	}
}
