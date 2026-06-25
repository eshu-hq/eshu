// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

func TestReportFailedOnRequiredFailure(t *testing.T) {
	var r Report
	r.AddCheck("drains", "fact_work_items_residual", true, true, "0 residual")
	r.AddCheck("graph", "rc-1", false, true, "0 edges, want >= 1")
	if !r.Failed() {
		t.Fatal("Failed() = false, want true when a required finding fails")
	}
}

func TestReportPassesWhenAdvisoryFails(t *testing.T) {
	var r Report
	r.AddCheck("drains", "fact_work_items_residual", true, true, "0 residual")
	r.AddCheck("graph", "node_count_Repository", false, false, "5, range [15,30]")
	if r.Failed() {
		t.Fatal("Failed() = true, want false when only an advisory finding fails")
	}
}

func TestReportEmptyIsFailure(t *testing.T) {
	var r Report
	if !r.Failed() {
		t.Fatal("empty report must fail: a gate that asserted nothing proved nothing")
	}
}

func TestReportWriteGroupsAndSummarizes(t *testing.T) {
	var r Report
	r.AddCheck("drains", "fact_work_items_residual", true, true, "0 residual")
	r.AddCheck("graph", "rc-1", false, true, "0 edges, want >= 1")
	r.AddCheck("graph", "node_count_Repository", false, false, "5, range [15,30]")

	var sb strings.Builder
	r.Write(&sb)
	out := sb.String()
	if !strings.Contains(out, "== drains ==") || !strings.Contains(out, "== graph ==") {
		t.Errorf("output missing phase headers:\n%s", out)
	}
	if !strings.Contains(out, "[FAIL] rc-1") {
		t.Errorf("required failure not marked FAIL:\n%s", out)
	}
	if !strings.Contains(out, "[WARN] node_count_Repository") {
		t.Errorf("advisory failure not marked WARN:\n%s", out)
	}
	if !strings.Contains(out, "1 pass, 1 required-fail, 1 advisory-warn") {
		t.Errorf("summary line wrong:\n%s", out)
	}
}
