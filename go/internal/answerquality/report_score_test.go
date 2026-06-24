// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerquality

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/serviceintel"
)

func failingCriteria(v ReportVerdict) []CriterionName {
	var out []CriterionName
	for _, criterion := range v.Criteria {
		if criterion.Status == CriterionFail {
			out = append(out, criterion.Name)
		}
	}
	return out
}

func TestScoreReportCorpus(t *testing.T) {
	for _, fixture := range ReportCorpus() {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			verdict := ScoreReport(fixture.Report)
			if verdict.Pass != fixture.ExpectPass {
				t.Fatalf("pass = %v, want %v (criteria: %+v)", verdict.Pass, fixture.ExpectPass, verdict.Criteria)
			}
			got := failingCriteria(verdict)
			if !sameCriteriaSet(got, fixture.ExpectFailing) {
				t.Fatalf("failing criteria = %v, want %v", got, fixture.ExpectFailing)
			}
		})
	}
}

func TestScoreReportVersionAndSubject(t *testing.T) {
	verdict := ScoreReport(honestCompleteReport())
	if verdict.Version != ReportEvidenceVersion {
		t.Fatalf("version = %q, want %q", verdict.Version, ReportEvidenceVersion)
	}
	if verdict.Subject != "checkout" {
		t.Fatalf("subject = %q, want checkout", verdict.Subject)
	}
	if verdict.Score != 100 {
		t.Fatalf("happy report score = %d, want 100", verdict.Score)
	}
}

func TestScoreReportIsDeterministic(t *testing.T) {
	a := ScoreReport(honestCompleteReport())
	b := ScoreReport(honestCompleteReport())
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("ScoreReport is not deterministic")
	}
}

func TestScoreReportPartialAnswerWithEvidenceIsHonest(t *testing.T) {
	// A partial section that still carries resolved evidence may keep its summary;
	// the scorecard must not flag it as a confident unsupported claim.
	verdict := ScoreReport(honestStaleReport())
	if !verdict.Pass {
		t.Fatalf("honest stale report should pass, got failures: %v", failingCriteria(verdict))
	}
}

func TestScoreReportEmptyReportIsHandled(t *testing.T) {
	// An empty report (no sections, no anchor) must score without panicking and
	// resolve every criterion deterministically. It makes no claims, so it has
	// nothing dishonest to reject; the composer never emits one in practice.
	verdict := ScoreReport(serviceintel.Report{})
	if len(verdict.Criteria) != len(reportCriteria) {
		t.Fatalf("verdict scored %d criteria, want %d", len(verdict.Criteria), len(reportCriteria))
	}
	if verdict.Version != ReportEvidenceVersion {
		t.Fatalf("empty report version = %q, want %q", verdict.Version, ReportEvidenceVersion)
	}
}

// sameCriteriaSet compares two criterion-name slices as sets.
func sameCriteriaSet(a, b []CriterionName) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[CriterionName]int{}
	for _, name := range a {
		seen[name]++
	}
	for _, name := range b {
		seen[name]--
	}
	for _, count := range seen {
		if count != 0 {
			return false
		}
	}
	return true
}
