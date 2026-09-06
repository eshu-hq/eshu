// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"slices"
	"testing"
)

func TestBoundedIndirectEvidenceHostnamesTrimsDeduplicatesAndCaps(t *testing.T) {
	t.Parallel()

	got, truncated := BoundedIndirectEvidenceHostnamesForService([]string{
		"",
		"api.qa.example.test",
		" api.qa.example.test ",
		"api.prod.example.test",
		"api.stage.example.test",
		"api.dev.example.test",
		"api.extra.example.test",
	}, "")

	want := []string{
		"api.dev.example.test",
		"api.prod.example.test",
		"api.qa.example.test",
		"api.stage.example.test",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("BoundedIndirectEvidenceHostnamesForService() = %#v, want %#v", got, want)
	}
	// #5720 round-7 P1-3: five distinct hostnames went in and
	// indirectEvidenceHostnameLimit kept four, so the dropped one has to be
	// disclosed -- a consumer repository reachable only through it is never
	// searched for.
	if !truncated {
		t.Fatal("truncated = false, want true (5 unique hostnames capped to indirectEvidenceHostnameLimit)")
	}
}

func TestBoundedIndirectEvidenceHostnamesPrefersServiceOwnedHosts(t *testing.T) {
	t.Parallel()

	got, truncated := BoundedIndirectEvidenceHostnamesForService([]string{
		"api.vendor.example.test",
		"docs.vendor.example.test",
		"checkout.qa.example.test",
		"metrics.vendor.example.test",
		"checkout.prod.example.test",
	}, "sample-checkout-api")

	want := []string{
		"checkout.prod.example.test",
		"checkout.qa.example.test",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("BoundedIndirectEvidenceHostnamesForService() = %#v, want %#v", got, want)
	}
	// #5720 round-8 P1-2: this assertion was inverted, and the reasoning behind
	// it ("the affinity filter selected rather than capped, so nothing was
	// lost") was the defect. The affinity filter is a drop path like any other.
	// It discarded three hostnames here, and a consumer repository that
	// references only api.vendor.example.test is never searched for and never
	// reaches the merged consumer set -- indistinguishable, from the caller's
	// side, from one dropped by indirectEvidenceHostnameLimit. The old
	// expectation let a service on a legacy or vanity domain report a
	// complete-looking consumer_repository_count with truncated: false.
	if !truncated {
		t.Fatalf(
			"truncated = false, want true (the affinity filter dropped %d of 5 hostnames before any consumer search ran)",
			5-len(got),
		)
	}
}
