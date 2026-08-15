// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerquality

import "github.com/eshu-hq/eshu/go/internal/answerguardrail"

// locatedString pairs a publishable value with a locator naming where it came
// from. It exists so a publish-safety failure can say WHERE the unsafe value
// was without saying WHAT it was, which is the contract
// answerguardrail.Finding documents for itself ("without echoing the unsafe
// value") and this package used to break.
type locatedString struct {
	// Where names the field, and for a captured result its surface, in a form
	// an operator can search their own evidence file for.
	Where string
	Value string
}

// firstUnsafeLocation returns the locator of the first value the shared
// publish-safety scanner rejects, or the empty string when all are safe. It
// deliberately returns the locator and never the value: a scorecard detail is
// printed to stdout, returned in the CLI's error to stderr, serialized into the
// --json verdict, and copied verbatim into a generated GitHub issue body, so a
// detail that quoted the value would publish exactly what the scorer refused.
func firstUnsafeLocation(values []locatedString) string {
	for _, value := range values {
		if answerguardrail.UnsafeString(value.Value) {
			return value.Where
		}
	}
	return ""
}

// screened returns value when the shared publish-safety scanner accepts it, and
// RedactedValue when it does not. It is the fallback for a captured field that
// is open-valued and so has no enum to fall back to -- a truth class is any
// string the capture tooling wrote. It is weaker than the enum labels by
// construction, because it can only be as good as the screen it calls, which is
// exactly why a field with a fixed set of legal values uses the set instead.
func screened(value string) string {
	if answerguardrail.UnsafeString(value) {
		return RedactedValue
	}
	return value
}

func aggregatePublishSafety(evidence Evidence, scores []PromptScore) CriterionScore {
	if where := firstUnsafeLocation([]locatedString{
		{Where: "run_id", Value: evidence.RunID},
		{Where: "eshu_commit", Value: evidence.EshuCommit},
	}); where != "" {
		return fail(CriterionPublishSafety, "unsafe run metadata in "+where)
	}
	return aggregatePromptCriterion(CriterionPublishSafety, scores)
}

func scorePublishSafety(prompt PromptResult) CriterionScore {
	if where := firstUnsafeLocation(promptLocatedStrings(prompt)); where != "" {
		return fail(CriterionPublishSafety, "unsafe publishable evidence in "+where)
	}
	return pass(CriterionPublishSafety, "evidence contains only redacted publishable strings")
}

// promptLocatedStrings returns every publishable string one captured prompt
// carries, each labelled with the field it came from so a publish-safety
// failure can point an operator at the row in their own evidence file without
// reprinting its contents.
func promptLocatedStrings(prompt PromptResult) []locatedString {
	values := []locatedString{
		{Where: "prompt id", Value: prompt.ID},
		{Where: "prompt family", Value: string(prompt.Family)},
		{Where: "prompt text", Value: prompt.Prompt},
		{Where: "expected_truth_class", Value: prompt.ExpectedTruthClass},
	}
	// required_surfaces was the one captured field the screen never saw. Each
	// RESULT's surface had a row, but the required list did not, so a surface
	// named there and never returned was screened by nothing -- and the parity
	// detail is the only place it is rendered. A carrier planted there shipped
	// in the --json verdict with the whole scorecard passing, which is worse
	// than the run-id case: there, at least publish_safety said no.
	for _, surface := range prompt.RequiredSurfaces {
		values = append(values, locatedString{Where: "required_surfaces", Value: string(surface)})
	}
	values = appendLocated(values, "required_next_calls", prompt.RequiredNextCalls)
	values = appendLocated(values, "acceptable_limitations", prompt.AcceptableLimitations)
	for _, result := range prompt.Results {
		// The locator names the surface through the enum, never through the
		// captured string. Concatenating the raw field built the WHERE half of
		// this contract out of an unscreened value: a locator is published in
		// full, so a surface carrying something the scanner misses would be
		// echoed verbatim through every following field's locator -- the same
		// leak this type exists to close, one field over. The raw value is
		// still screened, as its own row directly below.
		surface := result.Surface.label()
		values = append(
			values,
			locatedString{Where: "result surface", Value: string(result.Surface)},
			locatedString{Where: surface + " result answer_summary", Value: result.AnswerSummary},
			locatedString{Where: surface + " result truth_class", Value: result.TruthClass},
			locatedString{Where: surface + " result freshness", Value: result.Freshness},
		)
		values = appendLocated(values, surface+" result citation_handles", result.CitationHandles)
		values = appendLocated(values, surface+" result limitations", result.Limitations)
		values = appendLocated(values, surface+" result next_calls", result.NextCalls)
		values = appendLocated(values, surface+" result narration", narrationStrings(result.Narration))
	}
	return values
}

// appendLocated labels every value in a slice with one shared locator.
func appendLocated(values []locatedString, where string, raw []string) []locatedString {
	for _, value := range raw {
		values = append(values, locatedString{Where: where, Value: value})
	}
	return values
}
