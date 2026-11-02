// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"testing"
	"time"
)

// TestQuotaCriterionAcrossHourBoundary: a run that crosses an hour opens a
// new bucket, so held items may drain legitimately. No overshoot means no
// bucket above budget and at least one bucket driven to budget.
func TestQuotaCriterionAcrossHourBoundary(t *testing.T) {
	h18 := time.Date(2026, time.November, 2, 18, 0, 0, 0, time.UTC)
	h19 := h18.Add(time.Hour)
	at, below := Prefix+"scope-quota-at", Prefix+"scope-quota-below"
	cases := []struct {
		name    string
		buckets []QuotaBucket
		held    int
		want    string
	}{
		{"held within one hour", []QuotaBucket{{ScopeID: at, WindowStart: h18, Grants: 5}, {ScopeID: below, WindowStart: h18, Grants: 4}}, 2, StatusPass},
		{"drained across the hour", []QuotaBucket{{ScopeID: at, WindowStart: h18, Grants: 5}, {ScopeID: at, WindowStart: h19, Grants: 2}, {ScopeID: below, WindowStart: h18, Grants: 4}}, 0, StatusPass},
		{"bucket over budget", []QuotaBucket{{ScopeID: at, WindowStart: h18, Grants: 6}}, 1, StatusFail},
		{"budget never reached", []QuotaBucket{{ScopeID: at, WindowStart: h18, Grants: 3}, {ScopeID: below, WindowStart: h18, Grants: 4}}, 0, StatusFail},
		{"no buckets", nil, 0, StatusFail},
	}
	for _, tc := range cases {
		r := &runner{spec: Spec{QuotaBudget: 5}}
		report := &Report{Quota: tc.buckets, QuotaHeld: tc.held, Criteria: map[string]Criterion{}, NotApplicable: map[string]string{}}
		r.quotaCriterion(report)
		if got := report.Criteria["no_overshoot"]; got.Status != tc.want {
			t.Fatalf("%s: no_overshoot = %+v, want %s", tc.name, got, tc.want)
		}
	}
}
