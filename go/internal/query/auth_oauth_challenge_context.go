// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ErrBearerCredentialUnrecognized marks a bearer-credential denial where the
// presented credential was never a recognized issued token for this deployment
// — a JWT whose issuer is not in the active bearer-validation snapshot, or a
// pre-verify unparseable JWT (issue #5163, F-2). A ScopedTokenResolver wraps it
// with %w at exactly those PRE-match outcomes (see
// internal/oidcbearer.Resolver.deny) and never at a POST-match denial (expired,
// bad signature, wrong audience, malformed verified claims, no grants): a
// post-match denial means the credential WAS understood, so steering it to the
// discovery document would be noise. authMiddlewareWithRoutePolicy tests this
// sentinel with errors.Is to decide whether a resolver-error 401 augments its
// WWW-Authenticate challenge (rows 6/7) or stays bare (rows 5/11). An infra
// error from the resolver chain never carries this sentinel, so it fails safe
// to the bare challenge — the deliberate fail-safe against the
// anthropics/claude-code#59467 challenge-on-every-401 bug.
var ErrBearerCredentialUnrecognized = errors.New("query: bearer credential not a recognized issued token")

// requestWithOAuthChallenge forwards to querycontract.RequestWithOAuthChallenge.
// The implementation, and its context-key pair (oauthChallengePolicyFromContext,
// the unexported context key) and oauthWWWAuthenticateChallengeForRequest,
// moved there for #6642 as a types-only hoist alongside WriteUnauthorized,
// which needs them; every existing caller (auth.go) keeps its exact behavior
// through this wrapper.
func requestWithOAuthChallenge(r *http.Request, policy OAuthChallengePolicy) *http.Request {
	return querycontract.RequestWithOAuthChallenge(r, policy)
}
