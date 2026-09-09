// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
)

// TestCrossRepoDeadCodeConsumerReadPlan pins every shape the consumer lookup
// can take, because which list reaches the page decides where the row cap falls
// and whether the second traversal runs at all.
//
// Split out of code_dead_code_cross_repo_selector_test.go at the #6060 move:
// that file's other test needs the real root *ContentReader (which cannot
// move here without recreating ContentReader in this package), while this one
// exercises crossRepoDeadCodeConsumerReadPlan directly and has no root
// dependency at all.
func TestCrossRepoDeadCodeConsumerReadPlan(t *testing.T) {
	t.Parallel()

	scoped := repositoryAccessFilter{
		AllowedRepositoryIDs: []string{codeGrantGrantedRepo, codeGrantConsumerRepo},
		Allowed: map[string]struct{}{
			codeGrantGrantedRepo:  {},
			codeGrantConsumerRepo: {},
		},
	}
	unscoped := repositoryAccessFilter{AllScopes: true}

	cases := []struct {
		name      string
		access    repositoryAccessFilter
		consumers []string
		wantPage  []string
		wantSig   []string
		wantOK    bool
	}{
		{
			name:      "scoped request naming a consumer binds that consumer",
			access:    scoped,
			consumers: []string{codeGrantConsumerRepo},
			wantPage:  []string{codeGrantConsumerRepo},
			wantOK:    true,
		},
		{
			name:     "scoped request naming none binds the grant and probes its complement",
			access:   scoped,
			wantPage: []string{codeGrantConsumerRepo, codeGrantGrantedRepo},
			wantSig:  []string{codeGrantConsumerRepo, codeGrantGrantedRepo},
			wantOK:   true,
		},
		{
			name:      "unscoped request naming a consumer still binds it",
			access:    unscoped,
			consumers: []string{codeGrantOtherRepo},
			wantPage:  []string{codeGrantOtherRepo},
			wantOK:    true,
		},
		{
			name:   "unscoped request naming none is the one unbounded page",
			access: unscoped,
			wantOK: true,
		},
		{
			name:      "scoped request naming only ungranted consumers reads nothing",
			access:    scoped,
			consumers: []string{codeGrantOtherRepo},
			wantOK:    false,
		},
		{
			name:   "scoped caller with no grant at all reads nothing",
			access: repositoryAccessFilter{},
			wantOK: false,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reads, ok := deadcode.CrossRepoDeadCodeConsumerReadPlan(testCase.access, testCase.consumers)
			if ok != testCase.wantOK {
				t.Fatalf("ok = %v, want %v", ok, testCase.wantOK)
			}
			if !ok {
				if len(reads.PageRepositoryIDs) != 0 || len(reads.SignalGrant) != 0 {
					t.Fatalf("reads = %#v, want the zero plan; an unbounded read is not the fallback", reads)
				}
				return
			}
			if !slices.Equal(reads.PageRepositoryIDs, testCase.wantPage) {
				t.Fatalf("PageRepositoryIDs = %#v, want %#v", reads.PageRepositoryIDs, testCase.wantPage)
			}
			// An empty SignalGrant is the whole guard for a request that named
			// consumers: the probe answers over the complement of this list, so
			// running it with anything bound would report a repository the
			// request excluded. It is also what keeps a grantless caller from
			// a probe whose every range is empty.
			if !slices.Equal(reads.SignalGrant, testCase.wantSig) {
				t.Fatalf("SignalGrant = %#v, want %#v", reads.SignalGrant, testCase.wantSig)
			}
		})
	}
}
