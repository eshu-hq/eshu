// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

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
		want      k8sSelectMatchInput
	}{
		{
			name: "absent",
			candidate: K8sSelectCandidate{
				Kind: "Service", EntityName: "api", Namespace: "apps",
			},
			want: k8sSelectMatchInput{kind: "Service", name: "api", namespace: "apps"},
		},
		{
			name: "present empty",
			candidate: K8sSelectCandidate{
				Kind: "Service", EntityName: "api", Namespace: "apps",
				SelectorPresent: true, PodTemplateLabelsPresent: true,
			},
			want: k8sSelectMatchInput{
				kind: "Service", name: "api", namespace: "apps",
				selectorPresent: true, podTemplateLabelsPresent: true,
			},
		},
		{
			name: "present values",
			candidate: K8sSelectCandidate{
				Kind: "Service", EntityName: "api", Namespace: "apps",
				Selector: "app=api", SelectorPresent: true,
				PodTemplateLabels: "app=api", PodTemplateLabelsPresent: true,
			},
			want: k8sSelectMatchInput{
				kind: "Service", name: "api", namespace: "apps",
				selector: "app=api", selectorPresent: true,
				podTemplateLabels: "app=api", podTemplateLabelsPresent: true,
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

func TestCapabilityRegistrationOrderMatchesCanonicalYAMLOrder(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	parsed := loadCapabilityMatrixYAML(t, filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", "specs")))
	registrations := querycontract.CapabilityRegistrations()
	if got, want := len(registrations), 139; got != want {
		t.Fatalf("registered capabilities = %d, want %d", got, want)
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
