package semanticeval

import (
	"bytes"
	"math"
	"os"
	"strings"
	"testing"
)

func TestScoreRunComputesRetrievalMetricsAndTruthFailures(t *testing.T) {
	suite := Suite{
		Cases: []Case{
			{
				ID:       "auth-owner",
				Question: "what owns auth token validation?",
				Expected: []ExpectedHandle{
					{Handle: "file://auth.go", Relevance: 3, Required: true, MaxTruth: TruthClassExact},
					{Handle: "doc://auth-runbook", Relevance: 2, MaxTruth: TruthClassSemanticCandidate},
				},
				MustNotInclude: []string{"file://legacy-auth.go"},
			},
			{
				ID:       "deploy-owner",
				Question: "what deploys checkout?",
				Expected: []ExpectedHandle{
					{Handle: "svc://checkout", Relevance: 3, Required: true, MaxTruth: TruthClassExact},
				},
			},
		},
	}
	run := Run{
		Results: []CaseResult{
			{
				CaseID: "auth-owner",
				Candidates: []Candidate{
					{Handle: "doc://auth-runbook", Truth: TruthClassExact},
					{Handle: "file://auth.go", Truth: TruthClassExact},
					{Handle: "file://legacy-auth.go", Truth: TruthClassDerived},
					{Handle: "file://unrelated.go", Truth: TruthClassDerived},
				},
			},
			{
				CaseID: "deploy-owner",
				Candidates: []Candidate{
					{Handle: "svc://checkout-worker", Truth: TruthClassDerived},
					{Handle: "svc://checkout", Truth: TruthClassExact},
				},
			},
		},
	}

	report, err := Score(suite, run, Options{K: 3})
	if err != nil {
		t.Fatalf("Score() error = %v, want nil", err)
	}

	if got, want := report.CaseCount, 2; got != want {
		t.Fatalf("CaseCount = %d, want %d", got, want)
	}
	assertFloatNear(t, report.Averages.RecallAtK, 1.0, 0.0001)
	assertFloatNear(t, report.Averages.PrecisionAtK, 0.5, 0.0001)

	auth := report.Cases[0]
	assertFloatNear(t, auth.NDCGAtK, 0.9134, 0.0001)
	if got, want := auth.FalseCanonicalClaims, 1; got != want {
		t.Fatalf("auth FalseCanonicalClaims = %d, want %d", got, want)
	}
	if got, want := auth.ForbiddenHits, 1; got != want {
		t.Fatalf("auth ForbiddenHits = %d, want %d", got, want)
	}
}

func TestScoreRejectsInvalidEvalInputs(t *testing.T) {
	_, err := Score(Suite{Cases: []Case{
		{
			ID:       "duplicate",
			Question: "duplicate handles",
			Expected: []ExpectedHandle{
				{Handle: "file://a.go", Relevance: 1, Required: true, MaxTruth: TruthClassExact},
				{Handle: "file://a.go", Relevance: 1, MaxTruth: TruthClassDerived},
			},
		},
	}}, Run{}, Options{K: 10})
	if err == nil {
		t.Fatal("Score() error = nil, want duplicate expected handle error")
	}
	if !strings.Contains(err.Error(), "duplicate expected handle") {
		t.Fatalf("Score() error = %q, want duplicate expected handle", err.Error())
	}
}

func TestScoreRejectsUnknownRunCase(t *testing.T) {
	suite := Suite{Cases: []Case{
		{
			ID:       "known",
			Question: "known case",
			Expected: []ExpectedHandle{
				{Handle: "file://known.go", Relevance: 1, Required: true, MaxTruth: TruthClassExact},
			},
		},
	}}
	run := Run{Results: []CaseResult{
		{
			CaseID: "unknown",
			Candidates: []Candidate{
				{Handle: "file://known.go", Truth: TruthClassExact},
			},
		},
	}}

	_, err := Score(suite, run, Options{K: 10})
	if err == nil {
		t.Fatal("Score() error = nil, want unknown case error")
	}
	if !strings.Contains(err.Error(), "unknown case") {
		t.Fatalf("Score() error = %q, want unknown case", err.Error())
	}
}

func TestLoadRunJSONRejectsBlankCandidateHandle(t *testing.T) {
	_, err := LoadRunJSON(strings.NewReader(`{"results":[{"case_id":"case","candidates":[{"handle":"","truth":"exact"}]}]}`))
	if err == nil {
		t.Fatal("LoadRunJSON() error = nil, want blank candidate handle error")
	}
	if !strings.Contains(err.Error(), "candidate handle must not be blank") {
		t.Fatalf("LoadRunJSON() error = %q, want blank candidate handle", err.Error())
	}
}

func TestLoadJSONRejectsUnknownFields(t *testing.T) {
	_, err := LoadSuiteJSON(strings.NewReader(`{"cases":[{"id":"auth","question":"q","unknown":true}]}`))
	if err == nil {
		t.Fatal("LoadSuiteJSON() error = nil, want unknown field error")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("LoadSuiteJSON() error = %q, want unknown field", err.Error())
	}
}

func TestLoadJSONRejectsTrailingValues(t *testing.T) {
	_, err := LoadSuiteJSON(strings.NewReader(`{"cases":[]} {"cases":[]}`))
	if err == nil {
		t.Fatal("LoadSuiteJSON() error = nil, want trailing values error")
	}
	if !strings.Contains(err.Error(), "trailing values") {
		t.Fatalf("LoadSuiteJSON() error = %q, want trailing values", err.Error())
	}
}

func TestScoreCheckedInFixtureContract(t *testing.T) {
	suiteData, err := os.ReadFile("testdata/suite.json")
	if err != nil {
		t.Fatalf("os.ReadFile suite fixture: %v", err)
	}
	runData, err := os.ReadFile("testdata/current_run.json")
	if err != nil {
		t.Fatalf("os.ReadFile run fixture: %v", err)
	}

	suite, err := LoadSuiteJSON(bytes.NewReader(suiteData))
	if err != nil {
		t.Fatalf("LoadSuiteJSON() error = %v, want nil", err)
	}
	run, err := LoadRunJSON(bytes.NewReader(runData))
	if err != nil {
		t.Fatalf("LoadRunJSON() error = %v, want nil", err)
	}
	report, err := Score(suite, run, Options{K: 10})
	if err != nil {
		t.Fatalf("Score() error = %v, want nil", err)
	}

	assertFloatNear(t, report.Averages.RecallAtK, 1, 0.0001)
	assertFloatNear(t, report.Averages.PrecisionAtK, 0.15, 0.0001)
	assertFloatNear(t, report.Averages.NDCGAtK, 1, 0.0001)
	if got := report.Averages.FalseCanonicalClaims; got != 0 {
		t.Fatalf("FalseCanonicalClaims = %d, want 0", got)
	}
	if got := report.Averages.ForbiddenHits; got != 0 {
		t.Fatalf("ForbiddenHits = %d, want 0", got)
	}
	assertFloatNear(t, report.Averages.MeanLatencyMS, 59.5, 0.0001)
	assertFloatNear(t, report.Averages.P95LatencyMS, 77, 0.0001)
}

func assertFloatNear(t *testing.T, got float64, want float64, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Fatalf("got %.6f, want %.6f +/- %.6f", got, want, tolerance)
	}
}
