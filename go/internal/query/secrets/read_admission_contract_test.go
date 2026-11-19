// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secrets

import (
	"context"
	"reflect"
	"sort"
	"testing"
)

// frozenSecretsIAMReadAdmission pins the exact method set behind the
// versioned #9669 admission contract for the secrets/IAM reads: the
// four bounded list reads plus the bounded posture-summary rollup.
// The entries re-declare the store signatures in this
// secrets/IAM-scoped shape instead of embedding the five store ports,
// so no other family can cite this pin — embedding or aliasing any
// of the stores here would be a blanket pin, which the arbiter ruling
// forbids. A renamed, added, removed, or re-signed method fails here
// until that list is deliberately updated; bump
// SecretsIAMReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// Grant leg consumer: the SummarizeS3ExternalPrincipalGrantPosture
// grant read stays OUT of this pin (recorded in read_admission.go),
// so there is no grant method in the frozen set and a dest consumer
// cannot acquire graph reads through it.
var frozenSecretsIAMReadAdmission = []string{
	"ListSecretsIAMIdentityTrustChains func(context.Context, secrets.IAMIdentityTrustChainFilter) ([]secrets.IAMIdentityTrustChainRow, error)",
	"ListSecretsIAMPostureGaps func(context.Context, secrets.IAMPostureGapFilter) ([]secrets.IAMPostureGapRow, error)",
	"ListSecretsIAMPrivilegePostureObservations func(context.Context, secrets.IAMPrivilegePostureObservationFilter) ([]secrets.IAMPrivilegePostureObservationRow, error)",
	"ListSecretsIAMSecretAccessPaths func(context.Context, secrets.IAMSecretAccessPathFilter) ([]secrets.IAMSecretAccessPathRow, error)",
	"SummarizeSecretsIAMPosture func(context.Context, string) (secrets.IAMPostureSummary, error)",
}

// secretsIAMReadAdmissionWitness proves SecretsIAMReadAdmission is
// satisfiable: the five core-owned Postgres stores satisfy it jointly,
// composed by the consumer — no single store carries all five reads.
// It composes the existing store shapes rather than inventing a new
// satisfier, mirroring the IaC pin precedent.
type secretsIAMReadAdmissionWitness struct {
	PostgresIAMIdentityTrustChainStore
	PostgresIAMPrivilegePostureObservationStore
	PostgresIAMSecretAccessPathStore
	PostgresIAMPostureGapStore
	PostgresIAMPostureSummaryStore
}

var _ SecretsIAMReadAdmission = secretsIAMReadAdmissionWitness{}

// secretsIAMReadAdmissionMethodSet reflects the live
// SecretsIAMReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list. The iface parameter
// forces every caller — including the seeded-drift RED proof below —
// through this same guard comparison, so a passing suite proves the
// guard itself can see drift rather than proving a private copy can.
func secretsIAMReadAdmissionMethodSet(t *testing.T, iface any) []string {
	t.Helper()
	rt := reflect.TypeOf(iface).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestSecretsIAMReadAdmissionContractIsVersioned pins the #9669
// admission-contract version for the secrets/IAM reads. The frozen
// method set below is the enforcement: an added, removed, renamed,
// or re-signed method fails this test until the frozen list is
// deliberately updated. Bump
// SecretsIAMReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
func TestSecretsIAMReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if SecretsIAMReadAdmissionContractVersion != "v0" {
		t.Errorf("SecretsIAMReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", SecretsIAMReadAdmissionContractVersion, "v0")
	}
	got := secretsIAMReadAdmissionMethodSet(t, (*SecretsIAMReadAdmission)(nil))
	if len(got) != len(frozenSecretsIAMReadAdmission) {
		t.Fatalf("SecretsIAMReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenSecretsIAMReadAdmission, len(frozenSecretsIAMReadAdmission), SecretsIAMReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenSecretsIAMReadAdmission[i] {
			t.Errorf("SecretsIAMReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenSecretsIAMReadAdmission[i], SecretsIAMReadAdmissionContractVersion)
		}
	}
}

// seededSecretsIAMDrift is a deliberately widened copy of the pin:
// the five frozen secrets/IAM reads plus the S3 external-principal
// grant-posture read, which the grant leg keeps OUT of this pin (see
// read_admission.go). It never ships; it proves the frozen-set
// comparison below can see a one-method widening.
type seededSecretsIAMDrift interface {
	SecretsIAMReadAdmission
	ListSecretsIAMGrantPosture(context.Context, string) ([]string, error)
}

// TestSecretsIAMReadAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract
// test runs must report a mismatch on the planted widening. It passes
// on a clean tree precisely because the guard fires on drift — a
// guard that cannot see the seeded method is no guard.
func TestSecretsIAMReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := secretsIAMReadAdmissionMethodSet(t, (*seededSecretsIAMDrift)(nil))
	if reflect.DeepEqual(got, frozenSecretsIAMReadAdmission) {
		t.Fatal("seeded widening (ListSecretsIAMGrantPosture) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenSecretsIAMReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenSecretsIAMReadAdmission)+1)
	}
}
