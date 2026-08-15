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

// unsafeCarrier is the value planted into a captured field by the prompt
// metadata tests. It is a synthetic DSN, not a credential, and it is one the
// shared screen catches -- which matters for the fields below that have no enum
// to fall back on. See the limit noted on screened.
const unsafeCarrier = "bolt://neo4j:" + echoSentinel + "@graph.example.com:7687"

// promptMetadataPlants are the captured prompt fields the verdict renders
// somewhere a person or a downstream artifact sees: into a criterion detail,
// into a generated issue body, or straight into prompt_scores[] of the --json
// payload.
//
// Every one of them is a plain string unmarshalled from an evidence file, so
// none is validated and any can carry anything the capture tooling wrote. The
// scorer screened the run id and the per-result fields and stopped there, so an
// unsafe family or id failed publish_safety AND shipped in the same --json
// verdict -- the artifact whose whole purpose is to be safe to paste somewhere
// public. A criterion that refuses a value while the payload around it carries
// that value has refused nothing.
var promptMetadataPlants = map[string]func(*PromptResult){
	// Copied raw into PromptScore.Family and interpolated into every aggregate
	// criterion detail by aggregatePromptCriterion.
	"family": func(prompt *PromptResult) { prompt.Family = PromptFamily(unsafeCarrier) },
	// Copied raw into PromptScore.ID.
	"id": func(prompt *PromptResult) { prompt.ID = unsafeCarrier },
	// Rendered as the "want" half of the truth_honesty mismatch detail. The
	// observed half was screened; the expected half was not.
	"expected_truth_class": func(prompt *PromptResult) { prompt.ExpectedTruthClass = unsafeCarrier },
	// Rendered into the parity detail as a missing surface. This one was not
	// screened ANYWHERE: promptLocatedStrings covered each result's own surface
	// but never the required list, so a carrier here shipped with the whole
	// scorecard passing.
	"required_surfaces": func(prompt *PromptResult) {
		prompt.RequiredSurfaces = append(prompt.RequiredSurfaces, Surface(unsafeCarrier))
	},
	// Rendered into the follow_up_usefulness detail as a missing next call.
	"required_next_calls": func(prompt *PromptResult) {
		prompt.RequiredNextCalls = append(prompt.RequiredNextCalls, unsafeCarrier)
	},
}

// TestScoreDoesNotEchoUnsafePromptMetadata plants the carrier in one captured
// prompt field at a time and checks every rendered form of the verdict.
func TestScoreDoesNotEchoUnsafePromptMetadata(t *testing.T) {
	t.Parallel()

	for name, plant := range promptMetadataPlants {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			evidence := completeEvidence()
			plant(&evidence.Prompts[0])
			verdict := Score(evidence)

			// The screen has to SEE the field, or a clean payload only proves
			// the value never reached a renderer on this particular path.
			if got := verdict.Criterion(CriterionPublishSafety).Status; got != CriterionFail {
				t.Fatalf("publish_safety = %q, want fail for an unsafe %s", got, name)
			}
			assertVerdictDoesNotEcho(t, verdict, unsafeCarrier)
		})
	}
}

// TestScoreKeepsHonestPromptMetadataReadable is the negative control for the
// test above. Replacing every family, id, and expected truth class with a
// redaction marker would satisfy it and leave prompt_scores[] useless: an
// operator reads that array to find WHICH prompt failed.
func TestScoreKeepsHonestPromptMetadataReadable(t *testing.T) {
	t.Parallel()

	evidence := completeEvidence()
	verdict := Score(evidence)

	if got := verdict.Criterion(CriterionPublishSafety).Status; got != CriterionPass {
		t.Fatalf("publish_safety = %q, want pass on honest evidence", got)
	}
	if len(verdict.PromptScores) != len(evidence.Prompts) {
		t.Fatalf("PromptScores = %d, want %d", len(verdict.PromptScores), len(evidence.Prompts))
	}
	for i, score := range verdict.PromptScores {
		if score.ID != evidence.Prompts[i].ID {
			t.Fatalf("PromptScores[%d].ID = %q, want %q carried through untouched",
				i, score.ID, evidence.Prompts[i].ID)
		}
		if score.Family != evidence.Prompts[i].Family {
			t.Fatalf("PromptScores[%d].Family = %q, want %q carried through untouched",
				i, score.Family, evidence.Prompts[i].Family)
		}
	}
	if payload := rendered(t, verdict); strings.Contains(payload, RedactedValue) ||
		strings.Contains(payload, unrecognizedFamily) {
		t.Fatalf("honest evidence was redacted: %s", payload)
	}
}

// TestAggregatePromptCriterionNamesTheFamilyThroughTheEnum drives
// aggregatePromptCriterion directly, with a PromptScore whose Family was never
// labelled.
//
// It exists because the whole-Score path cannot reach this branch: scorePrompt
// labels the family before the score is ever aggregated, so breaking the label
// call here leaves every end-to-end test green. That makes the call
// unfalsifiable from outside, and an unfalsifiable guard is one a later edit
// deletes with the suite still passing. Aggregation reads a PromptScore it did
// not build, so it names the family the same way whoever built it should have.
func TestAggregatePromptCriterionNamesTheFamilyThroughTheEnum(t *testing.T) {
	t.Parallel()

	score := PromptScore{
		ID:     "unlabelled",
		Family: PromptFamily(unsafeCarrier),
		Criteria: []CriterionScore{
			{Name: CriterionParity, Status: CriterionFail, Detail: "required surfaces disagree"},
		},
	}
	got := aggregatePromptCriterion(CriterionParity, []PromptScore{score})

	if got.Status != CriterionFail {
		t.Fatalf("status = %q, want fail; nothing below is being measured", got.Status)
	}
	assertNoEcho(t, "aggregate parity detail", got.Detail, unsafeCarrier)
	if !strings.Contains(got.Detail, unrecognizedFamily) {
		t.Fatalf("detail = %q, want the unrecognized-family marker", got.Detail)
	}
	// The negative control, in the same test so the two cannot drift: a known
	// family still reads as itself.
	score.Family = PromptFamilyServiceStory
	known := aggregatePromptCriterion(CriterionParity, []PromptScore{score})
	if !strings.Contains(known.Detail, string(PromptFamilyServiceStory)) {
		t.Fatalf("detail = %q, want the known family named", known.Detail)
	}
}

// narrationPlants are the captured narration fields the narration_fallback
// detail renders. Every one of them already has a row in promptLocatedStrings,
// so the screen sees them and publish_safety fails -- and then the raw value was
// printed anyway, one criterion over, in the reason for the OTHER failure. Two
// criteria disagreeing about whether a value may be published is the same defect
// as the prompt metadata above, in a second file.
var narrationPlants = map[string]func(*NarrationComparison){
	"narration_status": func(n *NarrationComparison) {
		n.Status = NarrationStatus(unsafeCarrier)
	},
	"fallback_truth_class": func(n *NarrationComparison) {
		n.Fallback.TruthClass = unsafeCarrier
	},
	"fallback_freshness": func(n *NarrationComparison) {
		n.Fallback.Freshness = unsafeCarrier
	},
	"fallback_citation_handles": func(n *NarrationComparison) {
		n.Fallback.CitationHandles = append(n.Fallback.CitationHandles, unsafeCarrier)
	},
	// A non-empty fallback limitation list is enough to open the branch on its
	// own. Setting Fallback.Partial as well would trip the partial-mismatch
	// comparison first and this plant would never reach the renderer it targets.
	"fallback_limitations": func(n *NarrationComparison) {
		n.Fallback.Limitations = append(n.Fallback.Limitations, unsafeCarrier)
	},
	"fallback_next_calls": func(n *NarrationComparison) {
		n.Fallback.NextCalls = append(n.Fallback.NextCalls, unsafeCarrier)
	},
}

// TestScoreDoesNotEchoUnsafeNarration plants the carrier in one narration field
// at a time.
func TestScoreDoesNotEchoUnsafeNarration(t *testing.T) {
	t.Parallel()

	for name, plant := range narrationPlants {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			evidence := completeEvidence()
			narration := validNarrationComparison()
			plant(narration)
			evidence.Prompts[0].Results[0].Narration = narration
			verdict := Score(evidence)

			if got := verdict.Criterion(CriterionPublishSafety).Status; got != CriterionFail {
				t.Fatalf("publish_safety = %q, want fail for an unsafe %s", got, name)
			}
			assertVerdictDoesNotEcho(t, verdict, unsafeCarrier)
		})
	}
}

// TestScoreKeepsHonestNarrationDetailReadable is the negative control. A
// narration mismatch has to stay diagnosable: replacing every truth class and
// dropped citation with a marker would satisfy the test above and leave an
// operator with a failure they cannot act on.
func TestScoreKeepsHonestNarrationDetailReadable(t *testing.T) {
	t.Parallel()

	evidence := completeEvidence()
	narration := validNarrationComparison()
	narration.Fallback.CitationHandles = append(narration.Fallback.CitationHandles, "repo:dropped-by-narration")
	evidence.Prompts[0].Results[0].Narration = narration
	verdict := Score(evidence)

	criterion := verdict.Criterion(CriterionNarrationFallback)
	if criterion.Status != CriterionFail {
		t.Fatalf("narration_fallback = %q, want fail; nothing below is being measured", criterion.Status)
	}
	if !strings.Contains(criterion.Detail, "repo:dropped-by-narration") {
		t.Fatalf("detail = %q, want the honest dropped citation named", criterion.Detail)
	}
	if strings.Contains(criterion.Detail, RedactedValue) {
		t.Fatalf("an honest narration value was redacted: %q", criterion.Detail)
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
