// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidcbearer

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestDenyCarriesBoundedOutcomeForAudit proves a denial error exposes the
// outcome that caused it, so the governance audit trail can record why a
// credential was rejected instead of collapsing every failure into a generic
// reason (#5567).
//
// The outcome was previously formatted into the message only, and recovering it
// would have meant parsing an error string to fill an audit column.
func TestDenyCarriesBoundedOutcomeForAudit(t *testing.T) {
	t.Parallel()

	resolver := &Resolver{cache: &cache{}}
	for _, testCase := range []struct {
		outcome      string
		unrecognized bool
	}{
		{outcomeExpired, false},
		{outcomeWrongAudience, false},
		{outcomeBadSignature, false},
		{outcomeNoGrants, false},
		{outcomeJWKSFetchFailure, false},
		{outcomeUnknownIssuer, true},
		{outcomeMalformed, true},
	} {
		t.Run(testCase.outcome, func(t *testing.T) {
			t.Parallel()

			err := resolver.deny(context.Background(), "https://issuer.example.com", testCase.outcome, testCase.unrecognized)
			var carrier interface{ DenialOutcome() string }
			if !errors.As(err, &carrier) {
				t.Fatalf("deny(%q) error does not expose DenialOutcome: %v", testCase.outcome, err)
			}
			if got := carrier.DenialOutcome(); got != testCase.outcome {
				t.Fatalf("DenialOutcome() = %q, want %q", got, testCase.outcome)
			}
		})
	}
}

// TestDenyPreservesUnrecognizedWrappingAndMessage proves the typed denial error
// keeps the exact behavior the middleware already depends on: the
// unrecognized-credential sentinel still matches errors.Is (which decides
// whether the client is steered to the RFC 9728 discovery document), a
// post-match denial still does not, and both messages are unchanged.
func TestDenyPreservesUnrecognizedWrappingAndMessage(t *testing.T) {
	t.Parallel()

	resolver := &Resolver{cache: &cache{}}
	ctx := context.Background()

	unrecognized := resolver.deny(ctx, "https://issuer.example.com", outcomeUnknownIssuer, true)
	if !errors.Is(unrecognized, query.ErrBearerCredentialUnrecognized) {
		t.Fatal("unrecognized denial no longer matches ErrBearerCredentialUnrecognized")
	}
	wantUnrecognized := "oidcbearer: bearer token denied: unknown_issuer: " +
		query.ErrBearerCredentialUnrecognized.Error()
	if got := unrecognized.Error(); got != wantUnrecognized {
		t.Fatalf("unrecognized message = %q, want %q", got, wantUnrecognized)
	}

	postMatch := resolver.deny(ctx, "https://issuer.example.com", outcomeExpired, false)
	if errors.Is(postMatch, query.ErrBearerCredentialUnrecognized) {
		t.Fatal("post-match denial must NOT match ErrBearerCredentialUnrecognized")
	}
	if got, want := postMatch.Error(), "oidcbearer: bearer token denied: expired"; got != want {
		t.Fatalf("post-match message = %q, want %q", got, want)
	}
}
