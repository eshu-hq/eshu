// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
)

// stubContainerImageIdentityFencingTokenIssuer returns caller-controlled
// values in sequence, so a test can assert Handle stamps the write with
// whatever the issuer returns, independent of the host clock. Mirrors
// stubAWSCloudRuntimeDriftFencingTokenIssuer.
type stubContainerImageIdentityFencingTokenIssuer struct {
	tokens []int64
	err    error
	calls  int
}

func (s *stubContainerImageIdentityFencingTokenIssuer) NextContainerImageIdentityFencingToken(
	context.Context,
) (int64, error) {
	s.calls++
	if s.err != nil {
		return 0, s.err
	}
	if s.calls-1 < len(s.tokens) {
		return s.tokens[s.calls-1], nil
	}
	return s.tokens[len(s.tokens)-1], nil
}

// TestContainerImageIdentityHandlerRequiresFencingTokenIssuer proves the
// database-issued fencing token is a required adapter, mirroring
// TestAWSCloudRuntimeDriftHandlerRequiresFencingTokenIssuer: a nil issuer
// must not silently fall back to the host clock (#5874) -- that would
// silently reintroduce the wall-clock cross-replica skew vulnerability the
// sequence closes.
func TestContainerImageIdentityHandlerRequiresFencingTokenIssuer(t *testing.T) {
	t.Parallel()

	handler := ContainerImageIdentityHandler{
		FactLoader: &containerImageIdentityFenceProbeLoader{},
		Writer:     &recordingContainerImageIdentityWriter{},
		// FencingTokenIssuer deliberately left nil.
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-image-identity",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-git",
		SourceSystem: "git",
		Domain:       DomainContainerImageIdentity,
		Cause:        "test",
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want an error for a missing fencing token issuer")
	}
}

// TestContainerImageIdentityHandlerPropagatesFencingTokenIssuerError proves a
// database error issuing the token surfaces as a Handle() error rather than
// silently falling back to zero or the host clock.
func TestContainerImageIdentityHandlerPropagatesFencingTokenIssuerError(t *testing.T) {
	t.Parallel()

	issuer := &stubContainerImageIdentityFencingTokenIssuer{err: errors.New("sequence unavailable")}
	handler := ContainerImageIdentityHandler{
		FactLoader:         &containerImageIdentityFenceProbeLoader{},
		Writer:             &recordingContainerImageIdentityWriter{},
		FencingTokenIssuer: issuer,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-image-identity",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-git",
		SourceSystem: "git",
		Domain:       DomainContainerImageIdentity,
		Cause:        "test",
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want the issuer's error surfaced")
	}
	if issuer.calls != 1 {
		t.Fatalf("issuer.calls = %d, want 1", issuer.calls)
	}
}
