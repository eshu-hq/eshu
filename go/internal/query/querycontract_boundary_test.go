// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/kubernetes"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestQueryContractAliasesPreserveTypeIdentity(t *testing.T) {
	tests := []struct {
		name string
		root any
		leaf any
	}{
		{"query profile", QueryProfile(""), querycontract.QueryProfile("")},
		{"graph backend", GraphBackend(""), querycontract.GraphBackend("")},
		{"truth level", TruthLevel(""), querycontract.TruthLevel("")},
		{"truth basis", TruthBasis(""), querycontract.TruthBasis("")},
		{"freshness state", FreshnessState(""), querycontract.FreshnessState("")},
		{"truth freshness", TruthFreshness{}, querycontract.TruthFreshness{}},
		{"truth envelope", TruthEnvelope{}, querycontract.TruthEnvelope{}},
		{"error profiles", ErrorProfiles{}, querycontract.ErrorProfiles{}},
		{"error code", ErrorCode(""), querycontract.ErrorCode("")},
		{"error envelope", ErrorEnvelope{}, querycontract.ErrorEnvelope{}},
		{"response envelope", ResponseEnvelope{}, querycontract.ResponseEnvelope{}},
		{"answer truth class", AnswerTruthClass(""), querycontract.AnswerTruthClass("")},
		{"freshness cause", FreshnessCause(""), querycontract.FreshnessCause("")},
		{"freshness next check", FreshnessNextCheck{}, querycontract.FreshnessNextCheck{}},
		{"file content", FileContent{}, querycontract.FileContent{}},
		{"entity content", EntityContent{}, querycontract.EntityContent{}},
		{"k8s select candidate", K8sSelectCandidate{}, querycontract.K8sSelectCandidate{}},
		{"framework route", FrameworkRouteEvidence{}, querycontract.FrameworkRouteEvidence{}},
		{"framework route entry", FrameworkRouteEntryEvidence{}, querycontract.FrameworkRouteEntryEvidence{}},
		{"repository coverage", RepositoryContentCoverage{}, querycontract.RepositoryContentCoverage{}},
		{"repository language count", RepositoryLanguageCount{}, querycontract.RepositoryLanguageCount{}},
		{"repository entity type count", RepositoryEntityTypeCount{}, querycontract.RepositoryEntityTypeCount{}},
		{"repository language aggregate", RepositoryLanguageAggregate{}, querycontract.RepositoryLanguageAggregate{}},
		{"repository language repository", RepositoryLanguageRepository{}, querycontract.RepositoryLanguageRepository{}},
		{"repository language inventory", RepositoryLanguageInventoryRow{}, querycontract.RepositoryLanguageInventoryRow{}},
		{"repository catalog entry", RepositoryCatalogEntry{}, querycontract.RepositoryCatalogEntry{}},
		{"graph query port", (*GraphQuery)(nil), (*querycontract.GraphQuery)(nil)},
		{"content store port", (*ContentStore)(nil), (*querycontract.ContentStore)(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, want := reflect.TypeOf(tt.root), reflect.TypeOf(tt.leaf); got != want {
				t.Fatalf("root type %v differs from querycontract type %v", got, want)
			}
		})
	}
}

func TestK8sSelectCandidateConversionPreservesPresenceTriState(t *testing.T) {
	tests := []struct {
		name      string
		candidate K8sSelectCandidate
		want      kubernetes.SelectMatchInput
	}{
		{
			name: "absent",
			candidate: K8sSelectCandidate{
				Kind: "Service", EntityName: "api", Namespace: "apps",
			},
			want: kubernetes.SelectMatchInput{Kind: "Service", Name: "api", Namespace: "apps"},
		},
		{
			name: "present empty",
			candidate: K8sSelectCandidate{
				Kind: "Service", EntityName: "api", Namespace: "apps",
				SelectorPresent: true, PodTemplateLabelsPresent: true,
			},
			want: kubernetes.SelectMatchInput{
				Kind: "Service", Name: "api", Namespace: "apps",
				SelectorPresent: true, PodTemplateLabelsPresent: true,
			},
		},
		{
			name: "present values",
			candidate: K8sSelectCandidate{
				Kind: "Service", EntityName: "api", Namespace: "apps",
				Selector: "app=api", SelectorPresent: true,
				PodTemplateLabels: "app=api", PodTemplateLabelsPresent: true,
			},
			want: kubernetes.SelectMatchInput{
				Kind: "Service", Name: "api", Namespace: "apps",
				Selector: "app=api", SelectorPresent: true,
				PodTemplateLabels: "app=api", PodTemplateLabelsPresent: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := k8sSelectMatchInputFromCandidate(tt.candidate); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("conversion = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestCapabilityRegistrationOrderMatchesCanonicalYAMLOrder holds the Go
// capability registry and the checked-in capability matrix (the base file plus
// its fragments) to the same keys in the same order, in both directions. It
// carries no hand-written count: adding a capability needs its YAML row and its
// registration, and the test passes without a number bump.
//
// A capability removed from both sides at once passes this test, because the
// two sides agree. That removal is a published-contract change, and it does
// not land silently. The capability-inventory gate byte-compares the committed
// catalog artifact (go/internal/capabilitycatalog/data/catalog.generated.json)
// with a fresh build from the matrix, so the removal fails until the artifact
// is regenerated, and the entry disappears by name in the diff. For a
// capability with a positive claim, the replay-coverage gate also fails on the
// stale capability:<id> row in specs/replay-coverage-manifest.v1.yaml until
// the manifest and its committed dashboard are edited in the same change. A
// capability whose profiles are all unsupported has no manifest row; the
// catalog artifact is its guard. While a handler still names the capability,
// TestWriteGraphReadErrorCapabilitiesExistInMatrix fails too (#8318).
func TestCapabilityRegistrationOrderMatchesCanonicalYAMLOrder(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	parsed := loadCapabilityMatrixYAML(t, filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", "specs")))
	registrations := querycontract.CapabilityRegistrations()

	registered := make(map[string]struct{}, len(registrations))
	for _, registration := range registrations {
		registered[registration.Capability] = struct{}{}
	}
	declared := make(map[string]struct{}, len(parsed.Capabilities))
	for _, capability := range parsed.Capabilities {
		declared[capability.Capability] = struct{}{}
		if _, ok := registered[capability.Capability]; !ok {
			t.Errorf("capability %q is in the YAML matrix but not registered in Go", capability.Capability)
		}
	}
	for _, registration := range registrations {
		if _, ok := declared[registration.Capability]; !ok {
			t.Errorf("capability %q is registered in Go but not in the YAML matrix", registration.Capability)
		}
	}
	if t.Failed() {
		return
	}

	if got, want := len(parsed.Capabilities), len(registrations); got != want {
		t.Fatalf("YAML capabilities = %d, registrations = %d", got, want)
	}
	for i, capability := range parsed.Capabilities {
		if got := registrations[i].Capability; got != capability.Capability {
			t.Fatalf("registration[%d] = %q, want %q", i, got, capability.Capability)
		}
	}
	if duplicates := querycontract.DuplicateCapabilityRegistrations(); len(duplicates) != 0 {
		t.Fatalf("duplicate capability registration attempts = %v", duplicates)
	}
}
