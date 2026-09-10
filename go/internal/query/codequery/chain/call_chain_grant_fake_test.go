// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain_test

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/chain"
)

// Grant fixture ids for the moved call-chain tests. Values match the staying
// codequery grant tests that coined them (auth_scoped_code_topic_grant_test.go);
// a symbol declared in a _test.go file cannot be imported across a package
// boundary, so the moved family carries its own copy.
const (
	codeGrantGrantedRepo = "repo://tenant-a/granted-service"
	codeGrantOtherRepo   = "repo://tenant-b/other-service"
)

// Call-chain grant fixture names. Copy of auth_scoped_call_chain_grant_test.go,
// which stays in codequery: the moved white-box and cross-repo tests seed the
// same graph, and a _test.go symbol cannot be imported here.
const (
	callChainGrantedStart = "fn:call-chain-granted-start"
	callChainGrantedEnd   = "fn:call-chain-granted-end"
	callChainGrantedName  = "CallChainGrantedEnd"
	callChainUngrantedHop = "fn:call-chain-ungranted-hop"
	callChainUngrantedNam = "CallChainUngrantedHop"
)

// callChainGrantEntities is the two-tenant graph the grant proofs share.
// Copy of auth_scoped_call_chain_grant_test.go.
func callChainGrantEntities() []chain.GrantEntity {
	return []chain.GrantEntity{
		{
			UID: callChainGrantedStart, Name: "CallChainGrantedStart", RepoID: codeGrantGrantedRepo,
			Calls: []string{callChainGrantedEnd, callChainUngrantedHop},
		},
		{UID: callChainGrantedEnd, Name: callChainGrantedName, RepoID: codeGrantGrantedRepo},
		{UID: callChainUngrantedHop, Name: callChainUngrantedNam, RepoID: codeGrantOtherRepo},
	}
}

// containsPredicate reports whether any predicate in the list contains want.
// Copy of auth_scoped_call_chain_grant_test.go.
func containsPredicate(predicates []string, want string) bool {
	for _, predicate := range predicates {
		if strings.Contains(predicate, want) {
			return true
		}
	}
	return false
}
