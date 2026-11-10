// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenStatusHostedReadinessReadAdmission pins the exact method set behind
// the versioned #9669 admission contract for the Status hosted-readiness
// graph legs: exactly the Run and RunSingle reads the hosted-readiness route
// admits through — the repository-count leg (RunSingle over
// "MATCH (r:Repository) RETURN count(r) as count") and the
// workload-leftovers detection leg (Run through legacyworkload.Detect). A
// renamed, added, removed, or re-signed method fails here until that list is
// deliberately updated; bump StatusHostedReadinessReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape, keeping the eshu-api
// extraction boundary from drifting silently.
//
// D-wave consumer: Status mints follow-up, per the arbiter
// APPROVED-WITH-CONDITIONS ruling on #9669. The satellite records this pin
// in a follow-up; a satellite PR consuming an unminted pin is invalid.
// Arbiter review of this pin precedes satellite termination of the
// hosted-readiness graph legs.
//
// This pin is narrow by construction: it re-declares exactly the two graph
// reads in this Status-scoped shape instead of embedding the shared
// GraphQuery port, so no other family can cite this pin — pinning the
// shared port here would grant every family that reads through it, a blanket
// pin the arbiter ruling forbids. The GraphBackend selector is config, not
// a port, and the Detect algorithm is callee behavior, not port shape: there
// is no method set to freeze for either. This pin is not combined with the
// governance Summary pin: each admits only its own legs.
var frozenStatusHostedReadinessReadAdmission = []string{
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
	"RunSingle func(context.Context, string, map[string]interface {}) (map[string]interface {}, error)",
}

// statusHostedReadinessReadAdmissionMethodSet reflects the live
// StatusHostedReadinessReadAdmission method set into sorted "name
// signature" strings for comparison with the frozen list.
func statusHostedReadinessReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*StatusHostedReadinessReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestStatusHostedReadinessReadAdmissionContractIsVersioned pins the #9669
// admission-contract version for the Status hosted-readiness graph legs. The
// version value plus the frozen method set below are the enforcement: a
// breaking change to the admission shape fails this test until the frozen
// list is deliberately updated and the version bumps in the same diff.
func TestStatusHostedReadinessReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if StatusHostedReadinessReadAdmissionContractVersion != "v0" {
		t.Errorf("StatusHostedReadinessReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", StatusHostedReadinessReadAdmissionContractVersion, "v0")
	}
	got := statusHostedReadinessReadAdmissionMethodSet(t)
	if len(got) != len(frozenStatusHostedReadinessReadAdmission) {
		t.Fatalf("StatusHostedReadinessReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenStatusHostedReadinessReadAdmission, len(frozenStatusHostedReadinessReadAdmission), StatusHostedReadinessReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenStatusHostedReadinessReadAdmission[i] {
			t.Errorf("StatusHostedReadinessReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenStatusHostedReadinessReadAdmission[i], StatusHostedReadinessReadAdmissionContractVersion)
		}
	}
}

// TestStatusHostedReadinessReadAdmissionWireShapesAreSealed proves the seal
// on the pin: exactly the two re-declared graph reads, with no session,
// transaction, write, or snapshot surface riding along. The frozen
// method-set contract test pins the exact names; this test pins the
// exclusion the arbiter ruling demands.
func TestStatusHostedReadinessReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*StatusHostedReadinessReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 2 {
		t.Fatalf("StatusHostedReadinessReadAdmission has %d methods, want exactly 2: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := 0; i < rt.NumMethod(); i++ {
		name := rt.Method(i).Name
		for _, banned := range []string{"Session", "Transaction", "Write", "Snapshot", "Exec", "Summary", "List"} {
			if strings.Contains(name, banned) {
				t.Errorf("StatusHostedReadinessReadAdmission.%s contains %q: session, write, and snapshot legs stay out of this pin", name, banned)
			}
		}
	}
}
