// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// frozenIaCInventorySearchAdmission pins the exact method set behind the
// versioned #9669 W3 supplemental admission contract for the
// active-inventory search read behind GET /api/v0/iac/resources: the single
// InventoryStore.SearchActive read, re-declared here with the exported
// IaCInventorySearch shape so a satellite package can call it with a
// non-zero kind. A renamed, added, removed, or re-signed method fails here
// until the list is deliberately updated; bump
// IaCInventorySearchAdmissionContractVersion alongside (manually — the bump
// is convention, the frozen list is the enforcement) so dest consumers can
// pin the shape.
var frozenIaCInventorySearchAdmission = []string{
	"SearchActive func(context.Context, iac.IaCInventorySearch, querycontract.RepositoryAccessFilter) ([]iac.InventoryCandidate, error)",
}

// iaCInventorySearchAdmissionMethodSet reflects the live
// IaCInventorySearchAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func iaCInventorySearchAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*IaCInventorySearchAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestIaCInventorySearchAdmissionContractIsVersioned pins the #9669 W3
// supplemental admission-contract version for the active-inventory search
// read. The frozen method set is the enforcement: an added, removed,
// renamed, or re-signed method fails this test until the frozen list is
// deliberately updated.
func TestIaCInventorySearchAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if IaCInventorySearchAdmissionContractVersion != "v0" {
		t.Errorf("IaCInventorySearchAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", IaCInventorySearchAdmissionContractVersion, "v0")
	}
	got := iaCInventorySearchAdmissionMethodSet(t)
	if len(got) != len(frozenIaCInventorySearchAdmission) {
		t.Fatalf("IaCInventorySearchAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenIaCInventorySearchAdmission, len(frozenIaCInventorySearchAdmission), IaCInventorySearchAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenIaCInventorySearchAdmission[i] {
			t.Errorf("IaCInventorySearchAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenIaCInventorySearchAdmission[i], IaCInventorySearchAdmissionContractVersion)
		}
	}
}

// TestIaCInventorySearchKindCoversInternalKinds proves the exported kind seam
// tracks the internal kind set exactly: every label the Postgres store
// accepts resolves from an exported constant, and nothing else converts.
func TestIaCInventorySearchKindCoversInternalKinds(t *testing.T) {
	t.Parallel()

	for kind, label := range resourceKindLabels {
		exported := IaCInventorySearchKind(kind)
		internal, ok := IaCInventorySearch{Kind: exported}.toInventorySearch()
		if !ok {
			t.Errorf("exported kind %q does not convert", string(exported))
			continue
		}
		if internal.Kind != kind {
			t.Errorf("exported kind %q converts to %q, want %q", string(exported), internal.Kind, kind)
		}
		if got := resourceKindLabels[internal.Kind]; got != label {
			t.Errorf("converted kind %q maps to label %q, want %q", internal.Kind, got, label)
		}
	}
	if _, ok := (IaCInventorySearch{Kind: "terraform_state"}).toInventorySearch(); ok {
		t.Errorf("unknown kind converted: want rejection")
	}
	if _, ok := (IaCInventorySearch{}).toInventorySearch(); ok {
		t.Errorf("empty kind converted: want rejection")
	}
}

// TestIaCInventorySearchAdmissionWitnessComposesPostgresStore proves the
// witness delegates to the core-owned Postgres store: a valid search reaches
// the store, which fails here on its nil database.
func TestIaCInventorySearchAdmissionWitnessComposesPostgresStore(t *testing.T) {
	t.Parallel()

	witness := &iacInventorySearchAdmissionWitness{}
	if _, err := witness.SearchActive(t.Context(), IaCInventorySearch{Kind: IaCInventorySearchKindResource, Limit: 1}, querycontract.RepositoryAccessFilter{}); err == nil {
		t.Fatalf("valid search through nil-db store succeeded: want the store's database-required error")
	}
}

// TestIaCInventorySearchAdmissionWitnessRejectsUnknownKind proves unknown
// kinds never reach the store: the seam rejects them first.
// TestIaCInventorySearchMapsAllFields pins the exported-to-internal field
// mapping: every field of IaCInventorySearch must land on the same InventorySearch
// field (a Type/Provider swap must fail here, not pass silently).
func TestIaCInventorySearchMapsAllFields(t *testing.T) {
	t.Parallel()

	in := IaCInventorySearch{
		Kind:       IaCInventorySearchKindModule,
		Query:      "q-logging",
		Type:       "t-aws_s3_bucket",
		Provider:   "p-aws",
		Module:     "m-vpc",
		Repository: "r-infra",
		AfterName:  "n-tail",
		AfterID:    "i-9",
		Limit:      41,
	}
	got, ok := in.toInventorySearch()
	if !ok {
		t.Fatalf("toInventorySearch rejected a closed-set kind")
	}
	if got.Kind != resourceKindModule || got.Query != "q-logging" || got.Type != "t-aws_s3_bucket" ||
		got.Provider != "p-aws" || got.Module != "m-vpc" || got.Repository != "r-infra" ||
		got.AfterName != "n-tail" || got.AfterID != "i-9" || got.Limit != 41 {
		t.Fatalf("toInventorySearch = %#v, want field-for-field mapping of %#v", got, in)
	}
}

func TestIaCInventorySearchAdmissionWitnessRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	witness := &iacInventorySearchAdmissionWitness{}
	if _, err := witness.SearchActive(t.Context(), IaCInventorySearch{Kind: "terraform_state", Limit: 1}, querycontract.RepositoryAccessFilter{}); err == nil {
		t.Fatalf("unknown kind reached the store: want seam rejection")
	}
}
