// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
)

// stubAWSCloudRuntimeDriftFencingTokenIssuer returns caller-controlled
// values in sequence, so a test can assert Handle stamps the write with
// whatever the issuer returns, independent of the host clock.
type stubAWSCloudRuntimeDriftFencingTokenIssuer struct {
	tokens []int64
	err    error
	calls  int
}

func (s *stubAWSCloudRuntimeDriftFencingTokenIssuer) NextAWSCloudRuntimeDriftFencingToken(
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

// TestAWSCloudRuntimeDriftHandlerRequiresFencingTokenIssuer proves the
// database-issued fencing token is a required adapter, mirroring
// TestAWSCloudRuntimeDriftHandlerRequiresAdapters: a nil issuer must not
// silently fall back to the host clock (#5875 P1) -- that would silently
// reintroduce the exact clock-skew vulnerability this fix closes.
func TestAWSCloudRuntimeDriftHandlerRequiresFencingTokenIssuer(t *testing.T) {
	t.Parallel()

	intent := Intent{
		IntentID:     "intent-aws-drift-token",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "generation-aws",
		SourceSystem: "aws",
		Domain:       DomainAWSCloudRuntimeDrift,
	}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader: &stubAWSCloudRuntimeDriftEvidenceLoader{},
		Writer:         &stubAWSCloudRuntimeDriftFindingWriter{},
	}
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("Handle() error = nil, want missing fencing token issuer error")
	}
}

// TestAWSCloudRuntimeDriftHandlerStampsWriteWithIssuedFencingTokenNotHostClock
// is the P1 regression (#5875, round-5 hostile review, codex): fencingToken
// used to be evidenceAsOf.UnixMicro() -- the REDUCER HOST'S wall clock. With
// modest clock skew between reducer replicas, an older worker on a
// fast-clock host can carry a LARGER token than a later worker on a correct
// clock, so if the fresher worker commits first, the stale older worker is
// admitted afterward and its retire can replace correct truth with stale
// truth -- defeating the admission CAS's whole purpose.
//
// This test wires a clock that WOULD invert ordering if it were still the
// token source (a "fast" host reading a LATER wall-clock value on what is,
// by construction, actually the EARLIER logical pass), alongside an issuer
// stub returning EXPLICIT, controlled values. It asserts the write the
// handler hands to the writer is stamped with the ISSUER's value, never a
// value derived from h.Now(), proving the fix's actual mechanism (not just
// its outcome): ordering now depends solely on which pass's
// NextAWSCloudRuntimeDriftFencingToken call the database serviced more
// recently, never on any individual host's clock.
func TestAWSCloudRuntimeDriftHandlerStampsWriteWithIssuedFencingTokenNotHostClock(t *testing.T) {
	t.Parallel()

	// A "fast" host clock: if this were still driving the token (the bug),
	// it would produce a LARGER UnixMicro() value than any real deployment's
	// correctly-set clock would for the same logical moment -- exactly the
	// skew shape the fix must be immune to.
	fastSkewedNow := time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)
	issuer := &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{42}}
	loader := &stubAWSCloudRuntimeDriftEvidenceLoader{
		rows: []cloudruntime.AddressedRow{
			awsCloudRuntimeDriftOrphanedEvidenceRow("arn:aws:lambda:us-east-1:123456789012:function:token-source"),
		},
	}
	writer := &stubAWSCloudRuntimeDriftFindingWriter{}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:     loader,
		Writer:             writer,
		FencingTokenIssuer: issuer,
		Now:                func() time.Time { return fastSkewedNow },
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-token-source",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "gen-1",
		SourceSystem: "aws",
		Domain:       DomainAWSCloudRuntimeDrift,
		Cause:        "aws runtime resource facts observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1", writer.calls)
	}
	if issuer.calls != 1 {
		t.Fatalf("issuer.calls = %d, want 1: Handle must call the issuer exactly once per pass", issuer.calls)
	}
	if got, want := writer.write.FencingToken, int64(42); got != want {
		t.Fatalf(
			"write.FencingToken = %d, want %d (the issuer's value): got %d, which is what "+
				"fastSkewedNow.UnixMicro() would have produced if the host clock were still the "+
				"token source -- the exact regression this test guards against",
			got, want, got,
		)
	}
	if unwantedHostClockToken := fastSkewedNow.UnixMicro(); writer.write.FencingToken == unwantedHostClockToken {
		t.Fatalf(
			"write.FencingToken = %d matches fastSkewedNow.UnixMicro(): the host clock must never "+
				"be the fencing token source",
			writer.write.FencingToken,
		)
	}
}

// TestAWSCloudRuntimeDriftHandlerPropagatesFencingTokenIssuerError proves a
// failed token issuance aborts Handle before any evidence load or write,
// rather than falling back to an unfenced or zero-valued token.
func TestAWSCloudRuntimeDriftHandlerPropagatesFencingTokenIssuerError(t *testing.T) {
	t.Parallel()

	issuer := &stubAWSCloudRuntimeDriftFencingTokenIssuer{err: errors.New("sequence unavailable")}
	loader := &stubAWSCloudRuntimeDriftEvidenceLoader{}
	writer := &stubAWSCloudRuntimeDriftFindingWriter{}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:     loader,
		Writer:             writer,
		FencingTokenIssuer: issuer,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-token-error",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "gen-1",
		SourceSystem: "aws",
		Domain:       DomainAWSCloudRuntimeDrift,
		Cause:        "aws runtime resource facts observed",
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want the issuer's error propagated")
	}
	if loader.calls != 0 {
		t.Fatalf("loader.calls = %d, want 0: a failed token issuance must abort before the evidence load", loader.calls)
	}
	if writer.calls != 0 {
		t.Fatalf("writer.calls = %d, want 0", writer.calls)
	}
}
