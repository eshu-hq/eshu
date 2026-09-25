// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"regexp"
	"strings"
)

// catalogSweepSeededPlaceholders are the argument placeholders the sweep runner
// replaces with SEEDED subjects (the granted repository, its scope, the
// ungranted repository, the state scope). A seeded subject is by definition
// present, so a reason citing one cannot explain a missing-subject answer.
var catalogSweepSeededPlaceholders = []string{"$OTHER_REPO", "$STATE_SCOPE", "$SCOPE", "$REPO"}

// catalogSweepSeededLiterals are fragments of the seeded ids the runner mints.
// A checked-in literal argument containing one names a seeded subject too.
var catalogSweepSeededLiterals = []string{"e2e-seed-repo", "e2e-catalog-sweep"}

// catalogSweepCapabilityOutcomes are the accepted outcomes that describe the
// stack profile rather than a missing subject.
var catalogSweepCapabilityOutcomes = map[string]bool{
	"unsupported_capability":         true,
	"component_registry_unavailable": true,
	"ask_default_off":                true,
}

// catalogSweepCapabilityGate matches the profile switch a capability-only
// reason must name: an ESHU_* environment variable or the query profile /
// graph mode the outcome depends on.
var catalogSweepCapabilityGate = regexp.MustCompile(`ESHU_[A-Z0-9_]+|query profile|authoritative graph mode`)

// catalogSweepKnownOutcomes is the closed set of outcomes a case may accept:
// success, the typed not-found answers for an unseeded subject, and the
// capability answers a stack profile can legitimately give.
var catalogSweepKnownOutcomes = map[string]bool{
	"ok":                             true,
	"not_found":                      true,
	"scope_not_found":                true,
	"service_not_found":              true,
	"unsupported_capability":         true,
	"component_registry_unavailable": true,
	"ask_default_off":                true,
}

// acceptReasonProblem reports why a case's acceptReason cannot justify
// accepting anything other than "ok". A row that tolerates a non-ok answer
// stops proving the tool succeeds for its seeded subject, so the reason must
// name what is NOT seeded, never a seeded subject and never the tool alone:
//
//   - a reason that cites a seeded-subject placeholder ($REPO, $SCOPE, ...) is
//     rejected, because the placeholder names a subject that exists;
//   - a row that may answer a missing-subject outcome (not_found and kin) must
//     quote one of its own non-placeholder, non-seeded string arguments, which a
//     sentence a per-row template can stamp out cannot do;
//   - a row that accepts only capability outcomes must name the profile switch
//     (an ESHU_* variable, the query profile, or the graph mode) it depends on;
//   - every accepted outcome must be one catalogSweepKnownOutcomes lists.
//
// It returns "" when the reason is acceptable.
func acceptReasonProblem(tool string, c catalogSweepCase) string {
	reason := strings.TrimSpace(c.AcceptReason)
	if reason == "" {
		return "has no acceptReason"
	}
	subjectOutcome := false
	for _, outcome := range c.Accept {
		if !catalogSweepKnownOutcomes[outcome] {
			return "names the unknown outcome " + outcome
		}
		if outcome != "ok" && !catalogSweepCapabilityOutcomes[outcome] {
			subjectOutcome = true
		}
	}
	for _, placeholder := range catalogSweepSeededPlaceholders {
		if strings.Contains(reason, placeholder) {
			return "its acceptReason cites the seeded-subject placeholder " + placeholder + ", which names a subject that exists, not an unseeded one"
		}
	}
	if subjectOutcome {
		for _, value := range catalogSweepStringArguments(c.Arguments) {
			if len(value) >= 3 && !catalogSweepNamesSeededSubject(value) && strings.Contains(reason, value) {
				return ""
			}
		}
		return "its acceptReason quotes none of the case's own unseeded string arguments, so it does not name the unseeded subject of " + tool
	}
	if !catalogSweepCapabilityGate.MatchString(reason) {
		return "its acceptReason names no capability gate (an ESHU_* variable, the query profile, or the graph mode) for " + tool
	}
	return ""
}

// catalogSweepNamesSeededSubject reports whether a string argument is a
// placeholder or literal for a seeded subject.
func catalogSweepNamesSeededSubject(value string) bool {
	if strings.HasPrefix(value, "$") {
		return true
	}
	for _, literal := range catalogSweepSeededLiterals {
		if strings.Contains(value, literal) {
			return true
		}
	}
	return false
}

// catalogSweepStringArguments collects every string value in an argument tree.
func catalogSweepStringArguments(value any) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, item := range v {
			out = append(out, catalogSweepStringArguments(item)...)
		}
		return out
	case map[string]any:
		var out []string
		for _, item := range v {
			out = append(out, catalogSweepStringArguments(item)...)
		}
		return out
	default:
		return nil
	}
}
