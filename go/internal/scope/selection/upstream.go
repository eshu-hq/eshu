// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package selection

import "time"

// WithUpstream annotates s with the probe verdict (#8454). It applies only
// when s.Reason is not_listed: the classification and successor slug of the
// newest live not_listed row (newest evaluated_at wins, lowest selector id
// breaks ties, the same order Summarize uses for Reason), and whether that
// successor resolves to a selected scope. successorSelected decides the
// resolution at read time; it is called at most once, with the decider's
// successor slug, and never with an empty slug. Every other aggregate keeps
// no annotation: a verdict belongs to a not_listed row. observations must be
// the scope's live rows; rows that are not live never decide.
func (s Summary) WithUpstream(observations []Observation, successorSelected func(slug string) bool) Summary {
	if s.Reason != StateNotListed {
		return s
	}
	var (
		decider     Observation
		haveDecider bool
	)
	for _, o := range observations {
		if o.State != StateNotListed || !Live(o, s.EvaluatedAt) {
			continue
		}
		if !haveDecider || newerDecider(o, decider) {
			decider, haveDecider = o, true
		}
	}
	if !haveDecider {
		return s
	}
	s.Classification = decider.Classification
	s.SuccessorSlug = decider.SuccessorSlug
	s.SuccessorSelected = s.SuccessorSlug != "" && successorSelected != nil && successorSelected(s.SuccessorSlug)
	return s
}

// PageSelections is one page's selection annotation: the aggregate per
// scope id, plus the slug resolution the graph list path needs because its
// rows carry repo_slug but no scope id.
type PageSelections struct {
	ByScope     map[string]Summary
	ScopeBySlug map[string]string
}

// SummarizePage aggregates every scope id in scopes from its live rows: the
// same Summarize the single-scope freshness read uses, so a list entry and
// the freshness verdict for one scope can never disagree. A scope with no
// entry in live reads unknown. latestGeneration supplies rule (d)'s newest
// generation per scope and is called at most once per scope that needs it;
// successorSelected reports whether a successor slug resolves to a selected
// scope. An error from either fails the whole page: the caller never serves
// entries with evidence silently missing.
func SummarizePage(
	scopes []string,
	live map[string][]Observation,
	now time.Time,
	latestGeneration func(scopeID string) (time.Time, error),
	successorSelected func(slug string) bool,
) (map[string]Summary, error) {
	summaries := make(map[string]Summary, len(scopes))
	for _, scopeID := range scopes {
		observations := live[scopeID]
		summary, err := Summarize(observations, now, func() (time.Time, error) {
			return latestGeneration(scopeID)
		})
		if err != nil {
			return nil, err
		}
		summaries[scopeID] = summary.WithUpstream(observations, successorSelected)
	}
	return summaries, nil
}
