// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package demospec

import (
	"strings"
	"testing"
)

// TestConvertQuestionResultsFieldValidation proves the two eshu-hq/eshu#5566
// hard-fail branches convertQuestion added for demospec.ExpectedAnswer, the
// same fix applied to the golden snapshot's query_shapes.ResultsField: a
// positive minimum_results with no results_field must be rejected (the
// non-deterministic first-array-field selection #5566 removed), and a
// results_field naming a field that is not in required_response_fields must
// be rejected too. Both assertions check the error MESSAGE, not just that an
// error occurred, so an inverted condition, a typo in the error string, or a
// TrimSpace masking a whitespace-only results_field would fail this test
// rather than sail through silently.
func TestConvertQuestionResultsFieldValidation(t *testing.T) {
	t.Parallel()

	base := func() questionFile {
		return questionFile{
			ID:              "q_test",
			Question:        "does the fixture resolve?",
			CorrelationKind: "code_to_deployment",
			Surface:         surfaceFile{Kind: "mcp", Ref: "list_kubernetes_correlations"},
			ExpectedAnswer: expectedAnswerFile{
				RequiredResponseFields:  []string{"correlations", "count"},
				DemonstratesCorrelation: []string{"rc-4"},
			},
		}
	}

	cases := []struct {
		name        string
		mutate      func(qf *questionFile)
		wantErrText string
	}{
		{
			name: "minimum_results positive with no results_field",
			mutate: func(qf *questionFile) {
				qf.ExpectedAnswer.MinimumResults = 1
			},
			wantErrText: "no results_field naming which required_response_fields entry it counts",
		},
		{
			name: "minimum_results positive with whitespace-only results_field",
			mutate: func(qf *questionFile) {
				qf.ExpectedAnswer.MinimumResults = 1
				qf.ExpectedAnswer.ResultsField = "   "
			},
			wantErrText: "no results_field naming which required_response_fields entry it counts",
		},
		{
			name: "results_field not listed in required_response_fields",
			mutate: func(qf *questionFile) {
				qf.ExpectedAnswer.MinimumResults = 1
				qf.ExpectedAnswer.ResultsField = "not_listed"
			},
			wantErrText: "is not listed in required_response_fields",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			qf := base()
			tc.mutate(&qf)
			_, err := convertQuestion("manifest.yaml", qf)
			if err == nil {
				t.Fatalf("convertQuestion() error = nil, want an error containing %q", tc.wantErrText)
			}
			if !strings.Contains(err.Error(), tc.wantErrText) {
				t.Fatalf("convertQuestion() error = %q, want it to contain %q", err.Error(), tc.wantErrText)
			}
		})
	}

	// The positive control: a valid results_field naming a listed field must
	// convert cleanly, proving the two rejections above are not just an
	// always-fail bug in convertQuestion itself.
	t.Run("valid results_field converts cleanly", func(t *testing.T) {
		qf := base()
		qf.ExpectedAnswer.MinimumResults = 1
		qf.ExpectedAnswer.ResultsField = "correlations"
		q, err := convertQuestion("manifest.yaml", qf)
		if err != nil {
			t.Fatalf("convertQuestion() unexpected error = %v", err)
		}
		if q.ExpectedAnswer.ResultsField != "correlations" {
			t.Fatalf("ExpectedAnswer.ResultsField = %q, want %q", q.ExpectedAnswer.ResultsField, "correlations")
		}
	})
}
