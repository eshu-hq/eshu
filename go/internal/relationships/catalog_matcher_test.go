// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"fmt"
	"testing"
)

func TestMatchesEntryRequiresAliasBoundary(t *testing.T) {
	t.Parallel()

	entry := CatalogEntry{
		RepoID:  "repo-payments",
		Aliases: []string{"payments", "payments-api"},
	}

	tests := []struct {
		name      string
		candidate string
		want      string
	}{
		{
			name:      "exact alias",
			candidate: "payments",
			want:      "payments",
		},
		{
			name:      "case insensitive exact alias",
			candidate: "PAYMENTS",
			want:      "payments",
		},
		{
			name:      "url path segment",
			candidate: "https://github.com/acme/payments.git",
			want:      "payments",
		},
		{
			name:      "image path segment before tag delimiter",
			candidate: "ghcr.io/acme/payments:2026.06.13",
			want:      "payments",
		},
		{
			name:      "longer true alias wins",
			candidate: "payments-api",
			want:      "payments-api",
		},
		{
			name:      "hyphenated prefix is not a boundary",
			candidate: "company-payments",
			want:      "",
		},
		{
			name:      "hyphenated longer prefix is not a boundary",
			candidate: "company-payments-api",
			want:      "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := matchesEntry(tt.candidate, entry); got != tt.want {
				t.Fatalf("matchesEntry(%q) = %q, want %q", tt.candidate, got, tt.want)
			}
		})
	}
}

func TestMatchesEntryPreservesPrivateTerraformRegistryAlias(t *testing.T) {
	t.Parallel()

	entry := CatalogEntry{
		RepoID:  "repo-terraform-modules-aws",
		Aliases: []string{"terraform-modules-aws"},
	}

	got := matchesEntry("registry.example.com/platform/ecs-application/aws", entry)
	if got != "terraform-modules-aws" {
		t.Fatalf("matchesEntry() = %q, want terraform-modules-aws", got)
	}
}

func BenchmarkMatchCatalogLargeCatalog(b *testing.B) {
	catalog := make([]CatalogEntry, 0, 10000)
	for i := 0; i < 9999; i++ {
		catalog = append(catalog, CatalogEntry{
			RepoID: fmt.Sprintf("repo-service-%05d", i),
			Aliases: []string{
				fmt.Sprintf("service-%05d", i),
				fmt.Sprintf("github.com/acme/service-%05d", i),
			},
		})
	}
	catalog = append(catalog, CatalogEntry{
		RepoID:  "repo-payments-api",
		Aliases: []string{"payments-api", "github.com/acme/payments-api"},
	})
	candidates := []string{
		"ghcr.io/acme/service-00017:2026.06.13",
		"git::https://github.com/acme/service-01234.git//deploy?ref=main",
		"https://github.com/acme/payments-api.git",
		"company-payments-api",
		"registry.example.com/platform/ecs-application/aws",
		"unrelated-service",
		"quay.io/acme/service-09876:latest",
		"charts/service-04567",
	}
	matcher := newCatalogMatcher(catalog)

	b.ReportAllocs()
	for b.Loop() {
		seen := make(map[evidenceKey]struct{}, len(candidates))
		for _, candidate := range candidates {
			_ = matchCatalog(
				"repo-source",
				candidate,
				"main.tf",
				EvidenceKindTerraformModuleSource,
				RelUsesModule,
				0.98,
				"benchmark",
				"benchmark",
				matcher,
				seen,
				nil,
			)
		}
	}
}
