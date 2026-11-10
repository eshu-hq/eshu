// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secrets

import (
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
// strings for comparison with the frozen list.
func secretsIAMReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*SecretsIAMReadAdmission)(nil)).Elem()
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
	got := secretsIAMReadAdmissionMethodSet(t)
	if len(got) != len(frozenSecretsIAMReadAdmission) {
		t.Fatalf("SecretsIAMReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenSecretsIAMReadAdmission, len(frozenSecretsIAMReadAdmission), SecretsIAMReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenSecretsIAMReadAdmission[i] {
			t.Errorf("SecretsIAMReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenSecretsIAMReadAdmission[i], SecretsIAMReadAdmissionContractVersion)
		}
	}
}
