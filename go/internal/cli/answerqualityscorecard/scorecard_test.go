// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerqualityscorecard

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/answerquality"
)

func TestReadEvidenceReadsTheFileAtPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.json")
	if err := os.WriteFile(path, []byte(`{"version":"x"}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	raw, err := ReadEvidence(strings.NewReader("stdin must not win"), path)
	if err != nil {
		t.Fatalf("ReadEvidence() error = %v, want nil", err)
	}
	if string(raw) != `{"version":"x"}` {
		t.Fatalf("ReadEvidence() = %q, want the file contents", raw)
	}
}

func TestReadEvidenceFallsBackToStdinWhenPathIsBlank(t *testing.T) {
	for _, path := range []string{"", "   ", "\t\n"} {
		raw, err := ReadEvidence(strings.NewReader(`{"from":"stdin"}`), path)
		if err != nil {
			t.Fatalf("ReadEvidence(%q) error = %v, want nil", path, err)
		}
		if string(raw) != `{"from":"stdin"}` {
			t.Fatalf("ReadEvidence(%q) = %q, want the stdin contents", path, raw)
		}
	}
}

func TestReadEvidenceNamesTheUnreadableFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.json")

	_, err := ReadEvidence(strings.NewReader(""), missing)
	if err == nil {
		t.Fatal("ReadEvidence() error = nil, want a read failure")
	}
	// The operator has to know WHICH artifact could not be read; a bare
	// "no such file" leaves them guessing between --from and stdin.
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("ReadEvidence() error = %v, want it to name %q", err, missing)
	}
}

func TestRenderVerdictShowsThePassHeaderAndCriteria(t *testing.T) {
	var out bytes.Buffer
	RenderVerdict(&out, answerquality.Verdict{
		RunID: "redacted-run",
		Pass:  true,
		Score: 100,
		Criteria: []answerquality.CriterionScore{
			{Name: answerquality.CriterionUsefulness, Status: answerquality.CriterionPass, Detail: "all captured prompts passed"},
			{Name: answerquality.CriterionParity, Status: answerquality.CriterionNotMeasured, Detail: "no hosted capture"},
		},
	})

	got := out.String()
	for _, want := range []string{
		"Answer-quality scorecard PASSED",
		"run   : redacted-run",
		"score : 100",
		"[ok] usefulness: all captured prompts passed",
		"[--] parity: no hosted capture",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("RenderVerdict() output missing %q:\n%s", want, got)
		}
	}
}

func TestRenderVerdictShowsTheFailHeaderAndFollowUps(t *testing.T) {
	var out bytes.Buffer
	RenderVerdict(&out, answerquality.Verdict{
		Pass: false,
		Criteria: []answerquality.CriterionScore{
			{Name: answerquality.CriterionPublishSafety, Status: answerquality.CriterionFail, Detail: "unsafe publishable evidence"},
		},
		FollowUpIssues: []answerquality.FollowUpIssue{
			{Title: "Fix publish_safety", Labels: []string{"answer:dogfood", "capability:answer-experience"}},
		},
	})

	got := out.String()
	for _, want := range []string{
		"Answer-quality scorecard FAILED",
		"[!!] publish_safety: unsafe publishable evidence",
		"follow-up issues:",
		"- Fix publish_safety [answer:dogfood, capability:answer-experience]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("RenderVerdict() output missing %q:\n%s", want, got)
		}
	}
}

func TestRenderVerdictPlaceholdersAnEmptyRunID(t *testing.T) {
	for _, runID := range []string{"", "   "} {
		var out bytes.Buffer
		RenderVerdict(&out, answerquality.Verdict{RunID: runID, Pass: true})
		if !strings.Contains(out.String(), "run   : <repo>") {
			t.Fatalf("RenderVerdict(run_id=%q) did not use the placeholder:\n%s", runID, out.String())
		}
	}
}

func TestFailureSummaryJoinsEveryFailedCriterion(t *testing.T) {
	got := FailureSummary(answerquality.Verdict{
		Criteria: []answerquality.CriterionScore{
			{Name: answerquality.CriterionUsefulness, Status: answerquality.CriterionFail, Detail: "generic answer"},
			{Name: answerquality.CriterionParity, Status: answerquality.CriterionPass, Detail: "fine"},
			{Name: answerquality.CriterionBoundedness, Status: answerquality.CriterionFail, Detail: "unbounded"},
		},
	})

	want := "usefulness: generic answer; boundedness: unbounded"
	if got != want {
		t.Fatalf("FailureSummary() = %q, want %q", got, want)
	}
}

func TestFailureSummaryStaysHonestWithNoFailedCriterion(t *testing.T) {
	// Reached when the verdict fails for a reason the criteria list does not
	// carry. Inventing a specific cause here would be worse than saying so.
	got := FailureSummary(answerquality.Verdict{
		Criteria: []answerquality.CriterionScore{
			{Name: answerquality.CriterionParity, Status: answerquality.CriterionPass},
		},
	})
	if got != "unknown failure" {
		t.Fatalf("FailureSummary() = %q, want %q", got, "unknown failure")
	}
}

// sentinel is planted INSIDE a verdict value, never as a key and never as a
// whole field, so a rendering that copies only part of a string still trips it.
const sentinel = "S3NT1NEL6059CANARY"

// boundaryCarriers vary the character immediately before the sentinel. A
// screen or renderer that treats the sentinel differently depending on what
// precedes it -- the failure mode behind the private-host boundary-class bug,
// where every existing fixture happened to write a space -- shows up as one
// row of this table disagreeing with the others.
var boundaryCarriers = map[string]string{
	"start_of_string": sentinel,
	"space":           "dial tcp " + sentinel,
	"colon":           "svc:" + sentinel,
	"slash":           "path/" + sentinel,
	"quote":           `token "` + sentinel + `"`,
	"at":              "user@" + sentinel,
	"question":        "q?" + sentinel,
	"ampersand":       "a=1&" + sentinel,
}

// TestRenderVerdictCopiesOnlyTheDocumentedFieldsIntoText pins exactly which
// parts of a Verdict reach the human-readable rendering, because every one of
// them is a path by which a value copied out of the captured evidence artifact
// reaches an operator's terminal or a pasted report.
//
// It is a pin, not a safety proof: the fields marked wantRendered ARE printed
// today and that is the intended behavior. The test exists so that WIDENING
// the set -- printing prompt scores, or echoing a follow-up's detail -- cannot
// happen without someone changing this table and thinking about it.
func TestRenderVerdictCopiesOnlyTheDocumentedFieldsIntoText(t *testing.T) {
	cases := []struct {
		field        string
		plant        func(*answerquality.Verdict, string)
		wantRendered bool
	}{
		{
			field:        "run_id",
			plant:        func(v *answerquality.Verdict, s string) { v.RunID = s },
			wantRendered: true,
		},
		{
			field: "criteria.detail",
			plant: func(v *answerquality.Verdict, s string) {
				v.Criteria = []answerquality.CriterionScore{{Name: answerquality.CriterionUsefulness, Status: answerquality.CriterionFail, Detail: s}}
			},
			wantRendered: true,
		},
		{
			field: "follow_up.title",
			plant: func(v *answerquality.Verdict, s string) {
				v.FollowUpIssues = []answerquality.FollowUpIssue{{Title: s}}
			},
			wantRendered: true,
		},
		{
			field: "follow_up.labels",
			plant: func(v *answerquality.Verdict, s string) {
				v.FollowUpIssues = []answerquality.FollowUpIssue{{Title: "t", Labels: []string{s}}}
			},
			wantRendered: true,
		},
		{
			// Carried in the JSON verdict but deliberately not printed by the
			// text renderer, which shows the title and labels only.
			field: "follow_up.detail",
			plant: func(v *answerquality.Verdict, s string) {
				v.FollowUpIssues = []answerquality.FollowUpIssue{{Title: "t", Detail: s}}
			},
			wantRendered: false,
		},
		{
			// PromptScore.ID is a raw copy of the captured prompt's id. The
			// text renderer never walks PromptScores; only --json carries them.
			field: "prompt_scores.id",
			plant: func(v *answerquality.Verdict, s string) {
				v.PromptScores = []answerquality.PromptScore{{ID: s}}
			},
			wantRendered: false,
		},
	}

	planted := 0
	for _, tc := range cases {
		for boundary, carrier := range boundaryCarriers {
			verdict := answerquality.Verdict{Pass: true}
			tc.plant(&verdict, carrier)

			// A case whose plant did not take would assert nothing at all.
			if !strings.Contains(mustMarshalFields(t, verdict), sentinel) {
				t.Fatalf("%s/%s: sentinel never landed in the verdict", tc.field, boundary)
			}
			planted++

			var out bytes.Buffer
			RenderVerdict(&out, verdict)
			got := strings.Contains(out.String(), sentinel)

			if got != tc.wantRendered {
				t.Errorf("%s/%s: sentinel in text rendering = %t, want %t\n%s",
					tc.field, boundary, got, tc.wantRendered, out.String())
			}
		}
	}

	if want := len(cases) * len(boundaryCarriers); planted != want {
		t.Fatalf("planted %d sentinels, want %d -- the table did not run in full", planted, want)
	}
}

// mustMarshalFields renders every string a Verdict carries, so the plant check
// above verifies the sentinel reached the value rather than trusting the
// closure that set it.
func mustMarshalFields(t *testing.T, verdict answerquality.Verdict) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(verdict.RunID)
	for _, criterion := range verdict.Criteria {
		b.WriteString(criterion.Detail)
	}
	for _, score := range verdict.PromptScores {
		b.WriteString(score.ID)
	}
	for _, issue := range verdict.FollowUpIssues {
		b.WriteString(issue.Title)
		b.WriteString(issue.Detail)
		b.WriteString(strings.Join(issue.Labels, ""))
	}
	return b.String()
}
