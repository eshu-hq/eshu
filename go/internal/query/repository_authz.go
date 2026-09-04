// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// repositoryAccessFilter aliases the querycontract seam type so existing root
// call sites that only name the type (constructing a literal or passing a
// value) keep compiling unchanged. Methods cannot be aliased across packages
// in Go, so call sites invoking a method now call the exported querycontract
// method name directly (e.g. f.Scoped(), f.GraphParams(...)).
type repositoryAccessFilter = querycontract.RepositoryAccessFilter

// RepositoryAccessFilter is the exported spelling of repositoryAccessFilter
// (the same querycontract type). Exported impact-seam forwarders name it in
// their signatures so the impact family can move without touching callers.
// See #6060.
type RepositoryAccessFilter = querycontract.RepositoryAccessFilter

// repositoryAccessFilterFromContext resolves the request's AuthContext into a
// repositoryAccessFilter.
//
// This is a forwarder now. The constructor itself lives in querycontract beside
// the type it builds. It used to carry a comment explaining that it could not
// move because it depended on AuthContext, AuthContextFromContext and
// AuthModeShared, which were root concepts — that stopped being true when those
// three moved to queryauth, and the comment outlived the constraint it
// described. Go has no function aliases, so the ~185 call sites keep this
// unexported name rather than being rewritten.
func repositoryAccessFilterFromContext(ctx context.Context) repositoryAccessFilter {
	return querycontract.RepositoryAccessFilterFromContext(ctx)
}

// containsAuthString forwards to querycontract.ContainsAuthString so root call
// sites unrelated to repositoryAccessFilter (e.g. runtime-context grant
// checks) keep using the package-local name.
func containsAuthString(values []string, candidate string) bool {
	return querycontract.ContainsAuthString(values, candidate)
}
