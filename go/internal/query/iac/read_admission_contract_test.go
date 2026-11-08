// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"reflect"
	"sort"
	"testing"
)

// frozenIaCReadAdmission pins the exact method set behind the
// versioned #9669 O6 pin-3 admission contract for the IaC reads
// (GET /api/v0/iac/resources, GET /api/v0/replatforming/selectors,
// and the IaC POST family): the bounded inventory graph read (Run,
// RunSingle deliberately excluded), the inventory facet read
// (Summary), the selector inventory read
// (ListReplatformingSelectors), the two cloud-management finding
// reads (ListUnmanagedCloudResources, CountUnmanagedCloudResources),
// and the three materialized cleanup reads
// (ListLatestCleanupFindings, CountLatestCleanupFindings,
// HasLatestRows). The entries re-declare the shared-port signatures
// in this IaC-scoped shape instead of embedding the shared ports,
// so no other family can cite this pin — embedding or aliasing
// querycontract.GraphQuery, InventoryStore, ManagementStore,
// ReplatformingSelectorStore, or ReachabilityStore here would be a
// blanket pin, which the arbiter ruling forbids permanently. A
// renamed, added, removed, or re-signed method fails here until that
// list is deliberately updated; bump
// IaCReadAdmissionContractVersion alongside (manually — the bump
// is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// O6 consumer: IaC deliberate widened pin (O6 pin 3, AM-D4-IAC);
// the widened shape needs arbiter review per the Compare gap-04
// standing before satellite consumption.
var frozenIaCReadAdmission = []string{
	"CountLatestCleanupFindings func(context.Context, []string, []string, bool) (int, error)",
	"CountUnmanagedCloudResources func(context.Context, iac.ManagementFilter) (int, error)",
	"HasLatestRows func(context.Context, []string, []string) (bool, error)",
	"ListLatestCleanupFindings func(context.Context, []string, []string, bool, int, int) ([]iac.ReachabilityFindingRow, error)",
	"ListReplatformingSelectors func(context.Context, int, []string) (iac.ReplatformingSelectorPage, error)",
	"ListUnmanagedCloudResources func(context.Context, iac.ManagementFilter) ([]iac.ManagementFindingRow, error)",
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
	"Summary func(context.Context, querycontract.RepositoryAccessFilter, int) (iac.InventorySummary, error)",
}

// iaCReadAdmissionWitness proves IaCReadAdmission is satisfiable:
// the core-owned graph, inventory, management, selector, and
// reachability stores satisfy it jointly, composed by the consumer
// — no single store carries all eight methods. It composes the
// package's existing fakes rather than inventing a new satisfier.
type iaCReadAdmissionWitness struct {
	*stubIaCResourceGraph
	*stubIaCInventoryStore
	smokeManagementAndSelectorStore
	smokeReachabilityStore
}

var _ IaCReadAdmission = &iaCReadAdmissionWitness{}

// iaCReadAdmissionMethodSet reflects the live IaCReadAdmission
// method set into sorted "name signature" strings for comparison
// with the frozen list.
func iaCReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*IaCReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestIaCReadAdmissionContractIsVersioned pins the #9669 O6 pin-3
// admission-contract version for the IaC reads. The frozen method
// set below is the enforcement: an added, removed, renamed, or
// re-signed method fails this test until the frozen list is
// deliberately updated. Bump IaCReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
func TestIaCReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if IaCReadAdmissionContractVersion != "v0" {
		t.Errorf("IaCReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", IaCReadAdmissionContractVersion, "v0")
	}
	got := iaCReadAdmissionMethodSet(t)
	if len(got) != len(frozenIaCReadAdmission) {
		t.Fatalf("IaCReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenIaCReadAdmission, len(frozenIaCReadAdmission), IaCReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenIaCReadAdmission[i] {
			t.Errorf("IaCReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenIaCReadAdmission[i], IaCReadAdmissionContractVersion)
		}
	}
}
