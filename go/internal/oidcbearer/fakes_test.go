// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidcbearer

import (
	"context"
	"errors"

	"github.com/eshu-hq/eshu/go/internal/oidclogin"
)

// fakeProviderSource returns a fixed, mutable provider list. Tests mutate
// providers directly between calls to prove the TTL cache picks up CRUD.
type fakeProviderSource struct {
	providers []BearerProvider
	err       error
}

func (f *fakeProviderSource) ActiveBearerProviders(context.Context) ([]BearerProvider, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]BearerProvider, len(f.providers))
	copy(out, f.providers)
	return out, nil
}

// fakeGrantResolver maps a fixed set of group hashes to a fixed
// GrantResolution, exercising the real oidclogin.GrantResolver interface
// (not a reimplementation of it) exactly like the production
// fallbackOIDCGrantResolver composition does.
type fakeGrantResolver struct {
	// grantedForGroupHash maps one group hash to the resolution returned
	// when that hash is present in the query. Tests use a single-group
	// fixture so this stays a simple map instead of a rule engine.
	grantedForGroupHash string
	resolution          oidclogin.GrantResolution
	err                 error
	calls               int
}

func (f *fakeGrantResolver) ResolveGroupGrants(
	_ context.Context,
	query oidclogin.GrantQuery,
) (oidclogin.GrantResolution, bool, error) {
	f.calls++
	if f.err != nil {
		return oidclogin.GrantResolution{}, false, f.err
	}
	for _, hash := range query.GroupHashes {
		if hash == f.grantedForGroupHash {
			return f.resolution, true, nil
		}
	}
	return oidclogin.GrantResolution{}, false, nil
}

// errNoGrantsFixture is a sentinel error fakeGrantResolver can be configured
// to return, distinguishing "resolver errored" tests from "resolver found
// nothing" tests.
var errNoGrantsFixture = errors.New("fake grant resolver: forced failure")
