// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"reflect"
	"sort"
	"testing"
)

// frozenFreshnessReadAdmission pins the exact method set behind the
// versioned #9669 D13 Wave-1 admission contract for the Freshness
// family reads (the bounded generation-lifecycle page and the two
// bounded changed-since delta summaries). The entries re-declare the
// StatusStore signatures in this Freshness-scoped shape instead of
// embedding the GenerationLifecycleReader, ChangedSinceReader, or
// ServiceChangedSinceReader ports, so no other family can cite this
// pin. A renamed, added, removed, or re-signed method fails here
// until that list is deliberately updated; bump
// FreshnessReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// D-wave consumer: D13 Freshness-first Wave-1, per the arbiter Wave-1
// ruling on #9669.
var frozenFreshnessReadAdmission = []string{
	"ComputeChangedSinceDelta func(context.Context, changedsince.Filter) (changedsince.Summary, error)",
	"ComputeServiceChangedSinceDelta func(context.Context, changedsince.ServiceFilter) (changedsince.ServiceSummary, error)",
	"ListGenerationLifecycle func(context.Context, generation.LifecycleFilter) (generation.LifecyclePage, error)",
}

// freshnessReadAdmissionMethodSet reflects the live
// FreshnessReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func freshnessReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*FreshnessReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestFreshnessReadAdmissionContractIsVersioned pins the #9669 D13
// Wave-1 admission-contract version for the Freshness family reads.
// The version value plus the frozen method set below are the
// enforcement: a breaking change to the admission shape fails this
// test until the version bumps in the same diff.
func TestFreshnessReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if FreshnessReadAdmissionContractVersion != "v0" {
		t.Errorf("FreshnessReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", FreshnessReadAdmissionContractVersion, "v0")
	}
	got := freshnessReadAdmissionMethodSet(t)
	if len(got) != len(frozenFreshnessReadAdmission) {
		t.Fatalf("FreshnessReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenFreshnessReadAdmission, len(frozenFreshnessReadAdmission), FreshnessReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenFreshnessReadAdmission[i] {
			t.Errorf("FreshnessReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenFreshnessReadAdmission[i], FreshnessReadAdmissionContractVersion)
		}
	}
}
