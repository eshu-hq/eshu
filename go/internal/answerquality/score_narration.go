// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerquality

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/answernarration"
)

func scoreNarrationFallback(prompt PromptResult) CriterionScore {
	for _, result := range prompt.Results {
		if result.Narration == nil {
			continue
		}
		if detail := narrationFailure(result); detail != "" {
			return fail(CriterionNarrationFallback, fmt.Sprintf("%s result: %s", result.Surface.label(), detail))
		}
	}
	return pass(CriterionNarrationFallback, "optional narration preserves deterministic fallback rows")
}

func narrationFailure(result SurfaceResult) string {
	narration := result.Narration
	if narration == nil || narration.Status == NarrationStatusNotRequested {
		return ""
	}
	switch narration.Status {
	case NarrationStatusAccepted, NarrationStatusRejected, NarrationStatusUnavailable:
	default:
		// Through the enum. The status is unmarshalled from an evidence file, so
		// the value that lands here is by definition one the scorer does not
		// recognize -- printing it raw published an arbitrary captured string
		// through the verdict.
		return fmt.Sprintf("unknown narration status %q", narration.Status.label())
	}
	if detail := compareNarrationFallback(result, narration.Fallback); detail != "" {
		return detail
	}
	if narration.Status != NarrationStatusAccepted {
		return ""
	}
	if narration.ValidatorInput == nil {
		return "accepted narration missing validator input"
	}
	verdict := answernarration.Validate(*narration.ValidatorInput)
	if verdict.Valid {
		return ""
	}
	return "accepted narration failed validator: " + narrationFindingReasons(verdict.Findings)
}

// compareNarrationFallback reports how a narrated result diverges from the
// deterministic fallback row it must preserve, or "" when it preserves it.
//
// Every captured value it renders is screened. All of them -- both truth
// classes, both freshness values, and the dropped handle, limitation, and next
// call lists -- come from an evidence file with no validation, and this detail
// is copied into the --json verdict and into a generated issue body. Comparisons
// use the raw values; only the rendering is screened. These fields already have
// rows in promptLocatedStrings, so an unsafe one fails publish_safety too, and a
// verdict that fails that criterion while reprinting the value here has refused
// nothing.
func compareNarrationFallback(result SurfaceResult, fallback NarrationBaseline) string {
	if fallback.TruthClass != "" && result.TruthClass != fallback.TruthClass {
		return fmt.Sprintf("truth_class=%q fallback=%q",
			screened(result.TruthClass), screened(fallback.TruthClass))
	}
	if fallback.Freshness != "" && result.Freshness != fallback.Freshness {
		return fmt.Sprintf("freshness=%q fallback=%q",
			screened(result.Freshness), screened(fallback.Freshness))
	}
	if result.Supported != fallback.Supported {
		return fmt.Sprintf("supported=%t fallback=%t", result.Supported, fallback.Supported)
	}
	if result.Partial != fallback.Partial {
		return fmt.Sprintf("partial=%t fallback=%t", result.Partial, fallback.Partial)
	}
	if fallback.Truncated && !result.Truncated {
		return "truncated fallback hidden"
	}
	if missing := missingStrings(fallback.CitationHandles, result.CitationHandles); len(missing) > 0 {
		return "dropped fallback citations: " + strings.Join(screenAll(missing), ", ")
	}
	if fallback.Partial || fallback.Truncated || len(fallback.Limitations) > 0 {
		if missing := missingStrings(fallback.Limitations, result.Limitations); len(missing) > 0 {
			return "dropped fallback limitations: " + strings.Join(screenAll(missing), ", ")
		}
	}
	if missing := missingStrings(fallback.NextCalls, result.NextCalls); len(missing) > 0 {
		return "dropped fallback next calls: " + strings.Join(screenAll(missing), ", ")
	}
	return ""
}

// screenAll returns values with every entry the publish-safety scanner rejects
// replaced by RedactedValue. An honest entry is carried through untouched: the
// detail has to stay diagnosable, or the refusal has traded a leak for a failure
// nobody can act on.
func screenAll(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, screened(value))
	}
	return out
}

func missingStrings(required []string, actual []string) []string {
	actualSet := stringSet(actual)
	var missing []string
	for _, value := range required {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := actualSet[value]; !ok {
			missing = append(missing, value)
		}
	}
	return missing
}

func stringSet(values []string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	return set
}

func narrationFindingReasons(findings []answernarration.Finding) string {
	if len(findings) == 0 {
		return "unknown"
	}
	reasons := make([]string, 0, len(findings))
	for _, finding := range findings {
		reasons = append(reasons, string(finding.Reason))
	}
	return strings.Join(reasons, ", ")
}

func narrationStrings(narration *NarrationComparison) []string {
	if narration == nil {
		return nil
	}
	values := []string{
		string(narration.Status),
		narration.FallbackRef,
		narration.Fallback.TruthClass,
		narration.Fallback.Freshness,
	}
	values = append(values, narration.Fallback.CitationHandles...)
	values = append(values, narration.Fallback.Limitations...)
	values = append(values, narration.Fallback.NextCalls...)
	if narration.ValidatorInput != nil {
		values = append(values, narrationValidatorStrings(*narration.ValidatorInput)...)
	}
	return values
}

func narrationValidatorStrings(input answernarration.Input) []string {
	values := []string{
		input.Packet.PromptFamily,
		input.Packet.Question,
		input.Packet.PrimaryTool,
		input.Packet.PrimaryRoute,
		input.Packet.Summary,
		input.Packet.ResultRef,
		input.Packet.CitationRef,
	}
	values = append(values, input.Packet.Limitations...)
	values = append(values, input.Packet.UnsupportedReasons...)
	values = append(values, collectAnyStrings(input.Packet.Result)...)
	for _, nextCall := range input.Packet.RecommendedNextCalls {
		values = append(values, collectAnyStrings(nextCall)...)
	}
	values = append(values, input.CitationIDs...)
	for _, sentence := range input.Response.Sentences {
		values = append(values, sentence.Text, string(sentence.Kind))
		for _, ref := range sentence.Provenance {
			values = append(values, string(ref.Kind), ref.ID)
		}
	}
	return values
}

func collectAnyStrings(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return []string{typed}
	case []any:
		var values []string
		for _, item := range typed {
			values = append(values, collectAnyStrings(item)...)
		}
		return values
	case map[string]any:
		var values []string
		for key, item := range typed {
			values = append(values, key)
			values = append(values, collectAnyStrings(item)...)
		}
		return values
	default:
		return []string{fmt.Sprint(typed)}
	}
}
