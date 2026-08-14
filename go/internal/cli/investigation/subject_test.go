// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package investigation_test

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/investigation"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestParseSubjectFlags(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		raw     []string
		want    map[string]string
		wantErr bool
	}{
		{name: "empty input yields an empty scope", raw: nil, want: map[string]string{}},
		{
			name: "key=value pairs",
			raw:  []string{"advisory_id=GHSA-x", "package_id=pkg:npm/y"},
			want: map[string]string{"advisory_id": "GHSA-x", "package_id": "pkg:npm/y"},
		},
		{
			name: "surrounding whitespace is trimmed from both halves",
			raw:  []string{"  advisory_id  =  GHSA-x  "},
			want: map[string]string{"advisory_id": "GHSA-x"},
		},
		{
			name: "only the first = splits, so values may contain =",
			raw:  []string{"token=a=b=c"},
			want: map[string]string{"token": "a=b=c"},
		},
		{
			name: "a repeated key keeps the last value",
			raw:  []string{"scope_id=first", "scope_id=second"},
			want: map[string]string{"scope_id": "second"},
		},
		{name: "no separator", raw: []string{"noequals"}, wantErr: true},
		{name: "empty key", raw: []string{"=value"}, wantErr: true},
		{name: "empty value", raw: []string{"key="}, wantErr: true},
		{name: "whitespace-only value", raw: []string{"key=   "}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := investigation.ParseSubjectFlags(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseSubjectFlags(%q) = %v, want an error", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSubjectFlags(%q): %v", tc.raw, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseSubjectFlags(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseSubjectFlagsErrorNamesTheOffendingEntry(t *testing.T) {
	t.Parallel()

	_, err := investigation.ParseSubjectFlags([]string{"advisory_id=ok", "broken"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if got, want := err.Error(), `invalid --subject "broken": expected key=value`; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestSubjectOrPlaceholder(t *testing.T) {
	t.Parallel()

	t.Run("a populated scope passes through unchanged", func(t *testing.T) {
		t.Parallel()

		in := map[string]string{"scope_id": "s"}
		if got := investigation.SubjectOrPlaceholder(in); !reflect.DeepEqual(got, in) {
			t.Fatalf("SubjectOrPlaceholder(%v) = %v", in, got)
		}
	})

	for _, tc := range []struct {
		name string
		in   map[string]string
	}{
		{name: "nil scope", in: nil},
		{name: "empty scope", in: map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			want := map[string]string{"requested": "unspecified"}
			if got := investigation.SubjectOrPlaceholder(tc.in); !reflect.DeepEqual(got, want) {
				t.Fatalf("SubjectOrPlaceholder(%v) = %v, want %v", tc.in, got, want)
			}
		})
	}
}

func TestParseFamilyTrims(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		raw  string
		want query.InvestigationFamily
	}{
		{raw: "  supply_chain_impact  ", want: query.InvestigationFamilySupplyChainImpact},
		{raw: "drift", want: query.InvestigationFamilyDrift},
		{raw: "   ", want: query.InvestigationFamily("")},
		{raw: "not_a_family", want: query.InvestigationFamily("not_a_family")},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()

			if got := investigation.ParseFamily(tc.raw); got != tc.want {
				t.Fatalf("ParseFamily(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestBoundsFromMaxSourceFacts(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   int
		want *query.PacketBounds
	}{
		{name: "unset falls back to the contract default", in: 0, want: nil},
		{name: "negative falls back to the contract default", in: -3, want: nil},
		{name: "positive overrides", in: 5, want: &query.PacketBounds{MaxSourceFacts: 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := investigation.BoundsFromMaxSourceFacts(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("BoundsFromMaxSourceFacts(%d) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestSupplyChainFilterFromSubject(t *testing.T) {
	t.Parallel()

	subject := map[string]string{
		"finding_id":     "f",
		"advisory_id":    "a",
		"cve_id":         "c",
		"package_id":     "p",
		"repository_id":  "r",
		"subject_digest": "d",
		"unrelated":      "ignored",
	}
	want := query.SupplyChainImpactExplanationFilter{
		FindingID: "f", AdvisoryID: "a", CVEID: "c",
		PackageID: "p", RepositoryID: "r", SubjectDigest: "d",
	}
	if got := investigation.SupplyChainFilterFromSubject(subject); !reflect.DeepEqual(got, want) {
		t.Fatalf("SupplyChainFilterFromSubject = %+v, want %+v", got, want)
	}
}

func TestSupplyChainFilterHasScope(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		filter query.SupplyChainImpactExplanationFilter
		want   bool
	}{
		{name: "empty", filter: query.SupplyChainImpactExplanationFilter{}, want: false},
		{name: "finding id alone is enough", filter: query.SupplyChainImpactExplanationFilter{FindingID: "f"}, want: true},
		{name: "whitespace finding id is not", filter: query.SupplyChainImpactExplanationFilter{FindingID: "  "}, want: false},
		{name: "advisory without a target", filter: query.SupplyChainImpactExplanationFilter{AdvisoryID: "a"}, want: false},
		{name: "target without an advisory", filter: query.SupplyChainImpactExplanationFilter{PackageID: "p"}, want: false},
		{name: "advisory plus package", filter: query.SupplyChainImpactExplanationFilter{AdvisoryID: "a", PackageID: "p"}, want: true},
		{name: "cve plus repository", filter: query.SupplyChainImpactExplanationFilter{CVEID: "c", RepositoryID: "r"}, want: true},
		{name: "cve plus subject digest", filter: query.SupplyChainImpactExplanationFilter{CVEID: "c", SubjectDigest: "d"}, want: true},
		{
			name:   "whitespace-only halves do not satisfy the pair",
			filter: query.SupplyChainImpactExplanationFilter{AdvisoryID: " ", PackageID: "p"},
			want:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := investigation.SupplyChainFilterHasScope(tc.filter); got != tc.want {
				t.Fatalf("SupplyChainFilterHasScope(%+v) = %t, want %t", tc.filter, got, tc.want)
			}
		})
	}
}

func TestDeployableUnitParams(t *testing.T) {
	t.Parallel()

	t.Run("scope and generation are both required", func(t *testing.T) {
		t.Parallel()

		for _, subject := range []map[string]string{
			{},
			{"scope_id": "s"},
			{"generation_id": "g"},
			{"scope_id": "  ", "generation_id": "g"},
		} {
			if _, ok := investigation.DeployableUnitParams(subject); ok {
				t.Fatalf("DeployableUnitParams(%v) = ok, want a scope refusal", subject)
			}
		}
	})

	t.Run("domain is pinned to deployable_unit_correlation", func(t *testing.T) {
		t.Parallel()

		params, ok := investigation.DeployableUnitParams(map[string]string{"scope_id": "s", "generation_id": "g"})
		if !ok {
			t.Fatal("expected params")
		}
		if got := params.Get("domain"); got != "deployable_unit_correlation" {
			t.Fatalf("domain = %q", got)
		}
		if params.Has("anchor_kind") || params.Has("anchor_id") {
			t.Fatalf("no repository in scope must leave the anchor unset, got %v", params)
		}
	})

	t.Run("repository_id becomes a repository anchor", func(t *testing.T) {
		t.Parallel()

		params, ok := investigation.DeployableUnitParams(
			map[string]string{"scope_id": "s", "generation_id": "g", "repository_id": "r"})
		if !ok {
			t.Fatal("expected params")
		}
		if got := params.Get("anchor_kind"); got != "repository" {
			t.Fatalf("anchor_kind = %q", got)
		}
		if got := params.Get("anchor_id"); got != "r" {
			t.Fatalf("anchor_id = %q", got)
		}
	})

	t.Run("repo_id is accepted as an alias for repository_id", func(t *testing.T) {
		t.Parallel()

		params, _ := investigation.DeployableUnitParams(
			map[string]string{"scope_id": "s", "generation_id": "g", "repo_id": "r2"})
		if got := params.Get("anchor_id"); got != "r2" {
			t.Fatalf("anchor_id = %q, want the repo_id alias to apply", got)
		}
	})

	// The admission-decision store keys deployable-unit rows by repository
	// anchors, so a workload or service subject must not turn into an anchor
	// filter the backend cannot answer.
	t.Run("workload and service subjects do not become anchors", func(t *testing.T) {
		t.Parallel()

		params, _ := investigation.DeployableUnitParams(map[string]string{
			"scope_id": "s", "generation_id": "g",
			"workload_id": "w", "service_id": "svc",
		})
		if params.Has("anchor_kind") || params.Has("anchor_id") {
			t.Fatalf("workload/service subjects must stay packet context, got %v", params)
		}
	})
}

func TestDriftRequestBody(t *testing.T) {
	t.Parallel()

	t.Run("a scope is required", func(t *testing.T) {
		t.Parallel()

		for _, subject := range []map[string]string{{}, {"provider": "aws"}, {"scope_id": "   "}} {
			if _, ok := investigation.DriftRequestBody(subject); ok {
				t.Fatalf("DriftRequestBody(%v) = ok, want a scope refusal", subject)
			}
		}
	})

	for _, alias := range []string{"scope_id", "account_id", "project_id", "subscription_id"} {
		t.Run("scope alias "+alias, func(t *testing.T) {
			t.Parallel()

			body, ok := investigation.DriftRequestBody(map[string]string{alias: "scope-1"})
			if !ok {
				t.Fatalf("%s should satisfy the scope requirement", alias)
			}
			if body["scope_id"] != "scope-1" {
				t.Fatalf("scope_id = %v", body["scope_id"])
			}
		})
	}

	t.Run("scope_id wins over the provider aliases", func(t *testing.T) {
		t.Parallel()

		body, _ := investigation.DriftRequestBody(map[string]string{"account_id": "acct", "scope_id": "scope"})
		if body["scope_id"] != "scope" {
			t.Fatalf("scope_id = %v, want the explicit scope_id to take precedence", body["scope_id"])
		}
	})

	t.Run("optional provider and resource uid are carried when present", func(t *testing.T) {
		t.Parallel()

		body, _ := investigation.DriftRequestBody(map[string]string{
			"scope_id": "s", "provider": "aws", "cloud_resource_uid": "arn:aws:x",
		})
		if body["provider"] != "aws" || body["cloud_resource_uid"] != "arn:aws:x" {
			t.Fatalf("body = %v", body)
		}

		bare, _ := investigation.DriftRequestBody(map[string]string{"scope_id": "s"})
		if _, present := bare["provider"]; present {
			t.Fatalf("an absent provider must be omitted, got %v", bare)
		}
		if _, present := bare["cloud_resource_uid"]; present {
			t.Fatalf("an absent cloud_resource_uid must be omitted, got %v", bare)
		}
	})
}
