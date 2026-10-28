// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	vulnerabilitysuppressionv1 "github.com/eshu-hq/eshu-sdk/factschema/vulnerabilitysuppression/v1"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// frozenSuppressionAdmission pins the exact method set behind the versioned
// #8872 admission contract. A renamed, added, removed, or re-signed method
// fails here and must bump SuppressionAdmissionContractVersion in the same
// diff, so the eshu-api extraction boundary cannot drift silently.
var frozenSuppressionAdmission = []string{
	"UpsertVulnerabilitySuppression func(context.Context, v1.Suppression) (impact.VulnerabilitySuppressionMutationResult, error)",
}

// admissionMethodSet reflects the live SuppressionAdmission method set into
// sorted "name signature" strings for comparison with the frozen list.
func admissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*SuppressionAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestSuppressionAdmissionContractIsVersioned pins the #8872 admission-contract
// version for the single bounded in-API mutation (operator suppression
// upsert). The version value plus the frozen method set below are the
// enforcement: a breaking change to the admission shape fails this test until
// the version bumps in the same diff.
func TestSuppressionAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if SuppressionAdmissionContractVersion != "v0" {
		t.Errorf("SuppressionAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", SuppressionAdmissionContractVersion, "v0")
	}
	got := admissionMethodSet(t)
	if len(got) != len(frozenSuppressionAdmission) {
		t.Fatalf("SuppressionAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenSuppressionAdmission, len(frozenSuppressionAdmission), SuppressionAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenSuppressionAdmission[i] {
			t.Errorf("SuppressionAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenSuppressionAdmission[i], SuppressionAdmissionContractVersion)
		}
	}
}

// validAdmissionSuppression is an encodable operator suppression: encoding it
// succeeds, so a nil-storage admission must fail at the wrapper-level guard
// rather than anywhere deeper.
func validAdmissionSuppression() vulnerabilitysuppressionv1.Suppression {
	return vulnerabilitysuppressionv1.Suppression{
		SuppressionID: "admission-pin",
		Source:        "eshu_policy",
		Justification: "accepted_risk",
		Author:        "admission-contract-test",
		AuthoredAt:    "2026-10-28T00:00:00Z",
	}
}

// requireWrapperGuardError fails unless err is the wrapper-level nil-storage
// guard error. Deleting the guard in
// impact.PostgresVulnerabilitySuppressionMutationStore.UpsertVulnerabilitySuppression
// changes the outcome (a panic on the nil receiver, or a deeper-layer error
// on nil storage), so the guard cannot be removed without failing here.
func requireWrapperGuardError(t *testing.T, caseName string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: UpsertVulnerabilitySuppression = nil, want the wrapper-level nil-storage guard error", caseName)
	}
	if !strings.Contains(err.Error(), "vulnerability suppression mutation store is required") {
		t.Fatalf("%s: UpsertVulnerabilitySuppression error = %q, want the wrapper-level nil-storage guard text", caseName, err.Error())
	}
}

// TestPostgresStoreSatisfiesAdmissionContract proves the core-owned Postgres
// mutation store behind the admission port: it assigns to SuppressionAdmission
// and fails closed with the wrapper-level guard error (never a panic) when
// its storage is absent, so the API handler depends on the versioned contract
// rather than a direct table handle.
func TestPostgresStoreSatisfiesAdmissionContract(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	value := validAdmissionSuppression()

	var nilReceiver SuppressionAdmission = (*impact.PostgresVulnerabilitySuppressionMutationStore)(nil)
	_, err := nilReceiver.UpsertVulnerabilitySuppression(ctx, value)
	requireWrapperGuardError(t, "nil receiver", err)

	var nilStorage SuppressionAdmission = &impact.PostgresVulnerabilitySuppressionMutationStore{}
	_, err = nilStorage.UpsertVulnerabilitySuppression(ctx, value)
	requireWrapperGuardError(t, "nil storage", err)
}
