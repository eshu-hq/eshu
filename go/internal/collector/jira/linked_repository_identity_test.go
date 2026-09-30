// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// TestLinkedRepositoryIDEqualsIngesterRepositoryID proves the join key the
// story target-support read relies on (#7138): the id a Jira pull-request or
// merge-request link carries in linked_repository_id is the same id the git
// ingester stores for that repository, whatever remote spelling the ingester
// saw (SSH, HTTPS, with or without .git, mixed case). The story read matches a
// repository target's id against this payload key directly, so a mismatch here
// would silently attach no support to any repository.
func TestLinkedRepositoryIDEqualsIngesterRepositoryID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		link         ExternalLink
		ingesterSeen string
	}{
		{
			name: "github ssh remote",
			link: ExternalLink{
				Application: LinkApplication{Name: "GitHub", Type: "com.github"},
				Object:      LinkObject{URL: "https://github.com/o/r/pull/1"},
			},
			ingesterSeen: "git@github.com:o/r.git",
		},
		{
			name: "github https remote with .git",
			link: ExternalLink{
				Application: LinkApplication{Name: "GitHub", Type: "com.github"},
				Object:      LinkObject{URL: "https://github.com/o/r/pull/1"},
			},
			ingesterSeen: "https://github.com/o/r.git",
		},
		{
			name: "github mixed case owner",
			link: ExternalLink{
				Application: LinkApplication{Name: "GitHub", Type: "com.github"},
				Object:      LinkObject{URL: "https://github.com/Acme/Payments/pull/9"},
			},
			ingesterSeen: "git@github.com:acme/payments.git",
		},
		{
			name: "gitlab subgroup ssh remote",
			link: ExternalLink{
				Application: LinkApplication{Name: "GitLab", Type: "com.gitlab"},
				Object:      LinkObject{URL: "https://gitlab.com/g/sub/app/-/merge_requests/7"},
			},
			ingesterSeen: "git@gitlab.com:g/sub/app.git",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := linkedRepositoryID(tt.link)
			if got == "" {
				t.Fatalf("linkedRepositoryID(%q) = empty, want a canonical repository id", tt.link.Object.URL)
			}
			want, err := repositoryidentity.CanonicalRepositoryID(tt.ingesterSeen, "")
			if err != nil {
				t.Fatalf("CanonicalRepositoryID(%q) error = %v", tt.ingesterSeen, err)
			}
			if got != want {
				t.Fatalf("linkedRepositoryID(%q) = %q, ingester id for %q = %q; they must be equal", tt.link.Object.URL, got, tt.ingesterSeen, want)
			}
			metadata, err := repositoryidentity.MetadataFor("r", "", tt.ingesterSeen)
			if err != nil {
				t.Fatalf("MetadataFor(%q) error = %v", tt.ingesterSeen, err)
			}
			if got != metadata.ID {
				t.Fatalf("linkedRepositoryID(%q) = %q, MetadataFor(%q).ID = %q; they must be equal", tt.link.Object.URL, got, tt.ingesterSeen, metadata.ID)
			}
		})
	}
}
