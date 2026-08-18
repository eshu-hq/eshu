// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reposelector

import (
	"errors"
	"testing"
)

// fakeGetter serves a fixed listing, or a fixed error, in place of the CLI's
// HTTP client. It asserts nothing about paging: Resolve requests the listing
// with no limit and ignores ListResponse.Total, and a fake that pretended
// otherwise would document a guarantee the code does not make.
type fakeGetter struct {
	repos []Entry
	err   error
	path  string
}

func (f *fakeGetter) Get(path string, result any) error {
	f.path = path
	if f.err != nil {
		return f.err
	}
	out, ok := result.(*ListResponse)
	if !ok {
		return errors.New("unexpected result type")
	}
	*out = ListResponse{Repositories: f.repos}
	return nil
}

// TestResolve covers the exported entry point's branches against a fake Getter.
// The error strings are asserted whole rather than by substring: they are what
// an operator reads when a selector does not resolve, and the ambiguous case is
// already pinned byte-for-byte from the other side by
// TestRunAnalyzeDeadCodeFailsOnAmbiguousRepoSelector in go/cmd/eshu.
func TestResolve(t *testing.T) {
	listing := []Entry{
		{ID: "repository:r_one", Name: "payments", RepoSlug: "acme/payments"},
		{ID: "repository:r_two", Name: "billing", RepoSlug: "acme/billing"},
	}

	tests := []struct {
		name     string
		repos    []Entry
		getErr   error
		selector string
		want     string
		wantErr  string
	}{
		{
			name:     "exact name resolves to its id",
			repos:    listing,
			selector: "payments",
			want:     "repository:r_one",
		},
		{
			name:     "slug resolves to its id",
			repos:    listing,
			selector: "acme/billing",
			want:     "repository:r_two",
		},
		{
			name:     "id resolves to itself",
			repos:    listing,
			selector: "repository:r_one",
			want:     "repository:r_one",
		},
		{
			name:     "no match names the selector",
			repos:    listing,
			selector: "shipping",
			wantErr:  `resolve repo selector "shipping": no matching repository`,
		},
		{
			name:     "empty selector matches nothing",
			repos:    listing,
			selector: "",
			wantErr:  `resolve repo selector "": no matching repository`,
		},
		{
			name: "ambiguous selector names every match in sorted order",
			repos: []Entry{
				{ID: "repository:r_two", Name: "payments"},
				{ID: "repository:r_one", Name: "payments"},
			},
			selector: "payments",
			wantErr:  `resolve repo selector "payments": multiple repositories match: repository:r_one, repository:r_two`,
		},
		{
			// The same repository listed twice is one match, not an ambiguity.
			// Without the seen-set dedup this case would fail as ambiguous, so
			// it is the test that holds that branch in place.
			name: "duplicate entries for one id resolve rather than conflict",
			repos: []Entry{
				{ID: "repository:r_one", Name: "payments"},
				{ID: "repository:r_one", RepoSlug: "acme/payments"},
			},
			selector: "repository:r_one",
			want:     "repository:r_one",
		},
		{
			name:     "listing failure is wrapped with the selector",
			getErr:   errors.New("boom"),
			selector: "payments",
			wantErr:  `resolve repo selector "payments": boom`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeGetter{repos: tt.repos, err: tt.getErr}
			got, err := Resolve(client, tt.selector)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Resolve(%q) error = nil, want %q", tt.selector, tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("Resolve(%q) error = %q, want %q", tt.selector, err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve(%q) error = %v, want nil", tt.selector, err)
			}
			if got != tt.want {
				t.Fatalf("Resolve(%q) = %q, want %q", tt.selector, got, tt.want)
			}
			if client.path != "/api/v0/repositories" {
				t.Fatalf("Resolve requested %q, want %q", client.path, "/api/v0/repositories")
			}
		})
	}
}

// TestResolveRejectsNilClient pins the guard that the cobra wrapper depends on.
// A nil *APIClient boxed into a Getter is a non-nil interface and would slip
// this check, which is why go/cmd/eshu keeps its own nil check on the concrete
// pointer before calling Resolve.
func TestResolveRejectsNilClient(t *testing.T) {
	got, err := Resolve(nil, "payments")
	if err == nil {
		t.Fatalf("Resolve(nil) error = nil, want an error; got %q", got)
	}
	if want := `resolve repo selector "payments": missing API client`; err.Error() != want {
		t.Fatalf("Resolve(nil) error = %q, want %q", err.Error(), want)
	}
}
