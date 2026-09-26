// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// Verdict is the model's classification of one candidate name.
type Verdict string

const (
	// VerdictGluedCompound means the name glues two or more full words
	// together where naming.md rule 3 requires nesting instead (e.g.
	// "workloadinstance" instead of "workload/instance").
	VerdictGluedCompound Verdict = "glued_compound"
	// VerdictAcceptable means the name is a single lexical word, a
	// proper noun or product name, or an established idiom that rule 3
	// does not target.
	VerdictAcceptable Verdict = "acceptable"
)

// Disposition mirrors the severity/confidence/disposition/evidence finding
// shape AGENTS.md's Mandatory Pre-PR Code Review section already uses, so
// this gate's output composes directly with the rest of the review
// pipeline instead of introducing a parallel vocabulary.
type Disposition string

const (
	// DispositionBlock means the finding should stop the change (only
	// enforced where the caller has chosen to treat findings as
	// blocking; see the -blocking flag in main.go).
	DispositionBlock Disposition = "block"
	// DispositionInform means the finding is surfaced for owner
	// judgment, not enforced automatically.
	DispositionInform Disposition = "inform"
)

// Finding is one candidate name's classification.
type Finding struct {
	Path           string      `json:"path"`
	Name           string      `json:"name"`
	Kind           string      `json:"kind"` // "directory" today; see doc.go for scope.
	Verdict        Verdict     `json:"verdict"`
	Confidence     string      `json:"confidence"` // "high" | "medium" | "low"
	Disposition    Disposition `json:"disposition"`
	Evidence       string      `json:"evidence"`
	SuggestedSplit []string    `json:"suggested_split,omitempty"`
}

// Report is the full gate output for one invocation: every candidate the
// model classified, whether or not it stuttered. Emitting the full set
// (not just violations) lets a caller confirm the gate actually inspected
// the paths it claims to have covered.
type Report struct {
	Findings []Finding `json:"findings"`
}

// applyDisposition sets each finding's Disposition: DispositionBlock for a
// glued-compound finding when blocking is true (pre-commit mode), otherwise
// DispositionInform. The model does not set this field itself -- disposition
// depends on how THIS invocation was run (-blocking), not on the finding's
// content, so it is computed here rather than asked of the model.
func applyDisposition(r Report, blocking bool) Report {
	for i := range r.Findings {
		if r.Findings[i].Verdict == VerdictGluedCompound && blocking {
			r.Findings[i].Disposition = DispositionBlock
		} else {
			r.Findings[i].Disposition = DispositionInform
		}
	}
	return r
}

// Violations returns the findings the model classified as a glued
// compound, sorted by path for deterministic output.
func (r Report) Violations() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Verdict == VerdictGluedCompound {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// WriteJSON writes the report as JSON to w, one object, newline-terminated.
// This is the machine-consumable form other tooling (eshu-code-review, a
// PR-comment step) reads back in.
func (r Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteHuman writes a short human-readable summary of the violations to w,
// in the same "gate-name: path: reason" style verify-filename-stutter.sh
// uses, so failures read consistently across the repo's naming gates.
func WriteHuman(w io.Writer, violations []Finding) {
	for _, f := range violations {
		split := ""
		if len(f.SuggestedSplit) > 0 {
			split = fmt.Sprintf(" (suggest: %v)", f.SuggestedSplit)
		}
		_, _ = fmt.Fprintf(w, "naming-glue-gate: %s: %s [%s confidence]%s\n", f.Path, f.Evidence, f.Confidence, split)
	}
}
