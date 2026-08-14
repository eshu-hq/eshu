// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerquality

import (
	"encoding/json"
	"strings"
	"testing"
)

// answerguardrail.Finding documents itself as describing a failed guardrail
// "without echoing the unsafe value", and that package's README states it as an
// invariant because runtime callers put finding text straight into user-visible
// limitations. This package's publish-safety scorer did the opposite: it built
// "unsafe publishable evidence: " + unsafe, so the value the screen had just
// refused was printed to stdout, returned in the CLI's error to stderr, carried
// into the --json verdict, and -- for a prompt-level failure -- copied into a
// generated FollowUpIssue body.
//
// Observed on the origin/main binary at fc462effc:
//
//	eshu answer-quality-scorecard --from <evidence with 10.42.7.9 in a summary>
//	  [!!] publish_safety: service_story: unsafe publishable evidence:
//	       Graph backend reachable at 10.42.7.9 for the service.
//
// The scorecard's whole job on that path is to refuse to publish the value, so
// this package now follows the neighbouring contract: a failure names WHERE the
// unsafe value was found, never WHAT it was.

// echoSentinel is a synthetic marker, not a credential.
const echoSentinel = "S3NT1NEL"

// echoCarriers are values the widened screen rejects, one per rule, so the
// no-echo contract is proven against every rule rather than one representative.
var echoCarriers = map[string]string{
	"raw_ipv4":       "10.42.7." + "9",
	"http_url":       "http://graph.example.com/" + echoSentinel,
	"bolt_dsn":       "bolt://neo4j:" + echoSentinel + "@graph.example.com:7687",
	"schemeless_dsn": "svc:" + echoSentinel + "@host/tool",
	"bracketed_ipv6": "[fd00::1]:7687",
	"password_colon": "password: " + echoSentinel,
}

// TestScorePublishSafetyDoesNotEchoTheUnsafeValue covers the prompt-level
// scorer, whose Detail also becomes a generated GitHub issue body.
func TestScorePublishSafetyDoesNotEchoTheUnsafeValue(t *testing.T) {
	t.Parallel()

	for name, carrier := range echoCarriers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			evidence := completeEvidence()
			evidence.Prompts[0].Results[0].AnswerSummary = "Backend reachable at " + carrier + "."
			verdict := Score(evidence)

			criterion := verdict.Criterion(CriterionPublishSafety)
			if criterion.Status != CriterionFail {
				t.Fatalf("publish_safety = %q, want fail; nothing below is being measured", criterion.Status)
			}
			assertVerdictDoesNotEcho(t, verdict, carrier)
			// The failure still has to be actionable: it must name the surface
			// and field, or an operator cannot find the value in their own file.
			if !strings.Contains(criterion.Detail, "answer_summary") {
				t.Fatalf("detail = %q, want the offending field named", criterion.Detail)
			}
		})
	}
}

// TestScoreDoesNotEchoAnUnsafeSurface covers the field the locators are built
// out of. The scorer used to concatenate the captured surface into every
// following locator ("<surface> result answer_summary"), so a surface carrying
// an unsafe value was published in the WHERE half of a contract whose entire
// purpose is to publish a location instead of a value.
//
// The two carriers below are chosen to make the test mean something on its own.
// The first is caught by the shared screen, so the scorer would refuse it even
// with no fix, through the "result surface" row. The second is NOT caught by
// the screen -- it is honest-looking text -- which is why it is here: it proves
// the locator names the surface through the enum rather than relying on the
// screen to have caught whatever the surface carried, and it fails against a
// scorer that only screens.
func TestScoreDoesNotEchoAnUnsafeSurface(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		surface     string
		wantLocator string
	}{
		// The scanner catches this one, so the surface's own row fails first
		// and the locator is a fixed literal.
		"screened_by_the_scanner": {
			surface:     "bolt://neo4j:" + echoSentinel + "@graph.example.com:7687",
			wantLocator: "result surface",
		},
		// The scanner does NOT catch this one, so the surface's own row passes
		// and the failure lands on the next field -- whose locator is built out
		// of the surface. This is the case that fails against a scorer that
		// concatenates the raw field, and the reason the fix is an enum rather
		// than another call to the screen.
		"missed_by_the_scanner": {
			surface:     "internal-" + echoSentinel + "-surface",
			wantLocator: unrecognizedSurface + " result answer_summary",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			evidence := completeEvidence()
			evidence.Prompts[0].Results[0].Surface = Surface(testCase.surface)
			evidence.Prompts[0].Results[0].AnswerSummary = "Backend reachable at 10.42.7." + "9."
			verdict := Score(evidence)

			criterion := verdict.Criterion(CriterionPublishSafety)
			if criterion.Status != CriterionFail {
				t.Fatalf("publish_safety = %q, want fail; nothing below is being measured", criterion.Status)
			}
			assertVerdictDoesNotEcho(t, verdict, testCase.surface)
			// The refusal still has to be actionable, or the fix has traded a
			// leak for a locator nobody can use.
			if !strings.Contains(criterion.Detail, testCase.wantLocator) {
				t.Fatalf("detail = %q, want it to name %q", criterion.Detail, testCase.wantLocator)
			}
		})
	}
}

// TestScoreKeepsAKnownSurfaceReadable is the negative control for the test
// above. Replacing every surface with the marker would satisfy it and destroy
// the locator's purpose.
func TestScoreKeepsAKnownSurfaceReadable(t *testing.T) {
	t.Parallel()

	evidence := completeEvidence()
	evidence.Prompts[0].Results[0].AnswerSummary = "Backend reachable at 10.42.7." + "9."
	verdict := Score(evidence)

	criterion := verdict.Criterion(CriterionPublishSafety)
	if criterion.Status != CriterionFail {
		t.Fatalf("publish_safety = %q, want fail; nothing below is being measured", criterion.Status)
	}
	want := string(evidence.Prompts[0].Results[0].Surface) + " result answer_summary"
	if !strings.Contains(criterion.Detail, want) {
		t.Fatalf("detail = %q, want it to name %q", criterion.Detail, want)
	}
	if strings.Contains(rendered(t, verdict), unrecognizedSurface) {
		t.Fatalf("a known surface was replaced by the unrecognized marker: %s", rendered(t, verdict))
	}
}

// rendered returns every published form of the verdict as one string.
func rendered(t *testing.T, verdict Verdict) string {
	t.Helper()

	raw, err := json.Marshal(verdict)
	if err != nil {
		t.Fatalf("marshal verdict: %v", err)
	}
	return string(raw)
}

// TestAggregatePublishSafetyDoesNotEchoUnsafeRunMetadata covers the other
// caller, which screens run_id and eshu_commit. The run id is also copied into
// the verdict's own RunID field and printed by the CLI header, so refusing to
// echo it in the finding while carrying it in the header would be half a fix.
func TestAggregatePublishSafetyDoesNotEchoUnsafeRunMetadata(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"run_id", "eshu_commit"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			const carrier = "bolt://neo4j:" + echoSentinel + "@graph.example.com:7687"
			evidence := completeEvidence()
			if field == "run_id" {
				evidence.RunID = carrier
			} else {
				evidence.EshuCommit = carrier
			}
			verdict := Score(evidence)

			if got := verdict.Criterion(CriterionPublishSafety).Status; got != CriterionFail {
				t.Fatalf("publish_safety = %q, want fail", got)
			}
			assertVerdictDoesNotEcho(t, verdict, carrier)
			if !strings.Contains(verdict.Criterion(CriterionPublishSafety).Detail, field) {
				t.Fatalf("detail = %q, want %q named", verdict.Criterion(CriterionPublishSafety).Detail, field)
			}
			// An unsafe run id must read as a refusal, not as an absent field:
			// the CLI renders an empty run id as its first-run placeholder
			// "<repo>", which looks like a run id.
			if field == "run_id" && verdict.RunID != RedactedRunID {
				t.Fatalf("RunID = %q, want the explicit redaction marker %q", verdict.RunID, RedactedRunID)
			}
		})
	}
}

// TestVerdictKeepsASafeRunIDReadable is the negative control. Blanking every
// run id would satisfy the test above and destroy the field's purpose.
func TestVerdictKeepsASafeRunIDReadable(t *testing.T) {
	t.Parallel()

	evidence := completeEvidence()
	evidence.RunID = "ask-eshu-local-proof-redacted"
	verdict := Score(evidence)
	if verdict.RunID != evidence.RunID {
		t.Fatalf("RunID = %q, want %q carried through untouched", verdict.RunID, evidence.RunID)
	}
	if verdict.Criterion(CriterionPublishSafety).Status != CriterionPass {
		t.Fatalf("publish_safety = %q, want pass on honest metadata",
			verdict.Criterion(CriterionPublishSafety).Status)
	}
}

// assertVerdictDoesNotEcho checks every rendered form of the verdict a person
// or a downstream artifact can see: the criterion details, the follow-up issue
// bodies, the RunID the CLI prints in its header, and the whole --json payload.
func assertVerdictDoesNotEcho(t *testing.T, verdict Verdict, carrier string) {
	t.Helper()

	for _, criterion := range verdict.Criteria {
		assertNoEcho(t, "criterion "+string(criterion.Name), criterion.Detail, carrier)
	}
	for _, score := range verdict.PromptScores {
		for _, criterion := range score.Criteria {
			assertNoEcho(t, "prompt criterion "+string(criterion.Name), criterion.Detail, carrier)
		}
	}
	for _, issue := range verdict.FollowUpIssues {
		assertNoEcho(t, "follow-up title", issue.Title, carrier)
		assertNoEcho(t, "follow-up detail", issue.Detail, carrier)
	}
	assertNoEcho(t, "verdict run_id", verdict.RunID, carrier)

	raw, err := json.Marshal(verdict)
	if err != nil {
		t.Fatalf("marshal verdict: %v", err)
	}
	assertNoEcho(t, "--json payload", string(raw), carrier)
}

// assertNoEcho fails when text carries the unsafe value or its sentinel.
func assertNoEcho(t *testing.T, where, text, carrier string) {
	t.Helper()

	if strings.Contains(text, carrier) {
		t.Fatalf("%s echoes the unsafe value %q: %q", where, carrier, text)
	}
	if strings.Contains(text, echoSentinel) {
		t.Fatalf("%s echoes the sentinel from %q: %q", where, carrier, text)
	}
}
