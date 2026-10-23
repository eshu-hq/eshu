// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"slices"
	"strings"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/query/playbook"
)

// minPlaybookOverlap is the fewest distinct playbook title words an intent
// must share before find_tool attaches that playbook's call plan.
const minPlaybookOverlap = 2

// playbookStopWords are words too common in playbook titles and intents to
// count as a match.
var playbookStopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "from": true, "into": true,
	"this": true, "that": true, "what": true, "which": true, "show": true, "tell": true,
	"query": true, "context": true, "demo": true, "to": true,
}

// playbookPlan is the call plan find_tool attaches when a query playbook
// matches the intent.
type playbookPlan struct {
	ID             string                     `json:"id"`
	Name           string                     `json:"name"`
	Version        string                     `json:"version"`
	PromptFamily   string                     `json:"prompt_family"`
	RequiredInputs []string                   `json:"required_inputs"`
	Steps          []playbookStep             `json:"steps"`
	Resolved       *playbook.ResolvedPlaybook `json:"resolved,omitempty"`
}

// playbookStep names one tool the playbook calls, in order.
type playbookStep struct {
	ID   string `json:"id"`
	Tool string `json:"tool"`
}

// matchPlaybook returns the plan of the query playbook whose title words best
// cover the intent, or nil. The match is deterministic: the score is the
// share of the playbook's title words found in the intent, ties go to the
// earlier catalog entry, and a playbook with any step the caller may not call
// is skipped. When the anchors supply every required input by name, the plan
// also carries the resolver's fully specified calls.
func matchPlaybook(q Query, allowed func(string) bool) *playbookPlan {
	intent := wordSet(q.Intent)
	var best *playbook.Definition
	bestScore := 0.0
	catalog := playbook.Catalog()
	for i := range catalog {
		def := &catalog[i]
		title := wordSet(def.Name + " " + def.PromptFamily + " " + def.ID)
		overlap := 0
		for word := range title {
			if intent[word] {
				overlap++
			}
		}
		if overlap < minPlaybookOverlap || len(title) == 0 {
			continue
		}
		score := float64(overlap) / float64(len(title))
		if score <= bestScore || !stepsAllowed(*def, allowed) {
			continue
		}
		best, bestScore = def, score
	}
	if best == nil || bestScore < 0.5 {
		return nil
	}
	return planFor(*best, q.Anchors, allowed)
}

func stepsAllowed(def playbook.Definition, allowed func(string) bool) bool {
	for _, step := range def.Steps {
		if !allowed(step.Tool) {
			return false
		}
	}
	return true
}

func planFor(def playbook.Definition, anchors Anchors, allowed func(string) bool) *playbookPlan {
	plan := &playbookPlan{ID: def.ID, Name: def.Name, Version: def.Version, PromptFamily: def.PromptFamily}
	for _, in := range def.RequiredInputs {
		if in.Required {
			plan.RequiredInputs = append(plan.RequiredInputs, in.Name)
		}
	}
	for _, step := range def.Steps {
		plan.Steps = append(plan.Steps, playbookStep{ID: step.ID, Tool: step.Tool})
	}
	known := map[string]string{
		"repo_id": anchors.RepoID, "service_id": anchors.ServiceID,
		"workload_id": anchors.WorkloadID, "environment": anchors.Environment,
	}
	inputs := map[string]string{}
	for _, in := range def.RequiredInputs {
		if value := known[in.Name]; value != "" {
			inputs[in.Name] = value
		}
	}
	if resolved, err := def.Resolve(inputs); err == nil {
		// Drilldowns name tools too; keep only those the caller may be
		// offered, like the steps (matchPlaybook checks those).
		for i := range resolved.Calls {
			resolved.Calls[i].Drilldowns = slices.DeleteFunc(slices.Clone(resolved.Calls[i].Drilldowns),
				func(d playbook.Drilldown) bool { return !allowed(d.Tool) })
		}
		plan.Resolved = &resolved
	}
	return plan
}

// wordSet lowercases text and keeps distinct words of three or more letters
// that are not stop words.
func wordSet(text string) map[string]bool {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	set := make(map[string]bool, len(words))
	for _, word := range words {
		if len(word) >= 3 && !playbookStopWords[word] {
			set[word] = true
		}
	}
	return set
}
