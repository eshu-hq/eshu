// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"reflect"
	"testing"

	refOverlayV1 "github.com/eshu-hq/eshu/sdk/go/factschema/ref_overlay/v1"
)

// TestDecodeRefOverlayBranchObservation_RoundTrip proves the typed seam
// carries a full branch observation through the envelope payload shape and
// back without loss.
func TestDecodeRefOverlayBranchObservation_RoundTrip(t *testing.T) {
	t.Parallel()

	sha := "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	want := refOverlayV1.BranchObservation{
		RepoID:        "github.com/eshu-hq/eshu",
		Ref:           "env/prod",
		RefKind:       refOverlayV1.RefKindBranch,
		Resolvability: refOverlayV1.ResolvabilityMobile,
		HeadSHA:       &sha,
	}
	payload, err := EncodeRefOverlayBranchObservation(want)
	if err != nil {
		t.Fatalf("EncodeRefOverlayBranchObservation error = %v, want nil", err)
	}
	got, err := DecodeRefOverlayBranchObservation(Envelope{FactKind: FactKindRefOverlayBranchObservation, SchemaVersion: "1.0.0", Payload: payload})
	if err != nil {
		t.Fatalf("DecodeRefOverlayBranchObservation error = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, want)
	}
}

// TestDecodeConfigKeyDivergence_RoundTrip proves the typed seam carries a
// divergence row — keys plus a per-env boolean, no values — through the
// envelope payload shape and back without loss.
func TestDecodeConfigKeyDivergence_RoundTrip(t *testing.T) {
	t.Parallel()

	valueKind := refOverlayV1.ValueKindURL
	want := refOverlayV1.ConfigKeyDivergence{
		RepoID:    "github.com/eshu-hq/eshu",
		Ref:       "env/prod",
		ConfigKey: "service.api_endpoint",
		Env:       "prod",
		Diverged:  true,
		ValueKind: &valueKind,
	}
	payload, err := EncodeConfigKeyDivergence(want)
	if err != nil {
		t.Fatalf("EncodeConfigKeyDivergence error = %v, want nil", err)
	}
	got, err := DecodeConfigKeyDivergence(Envelope{FactKind: FactKindConfigKeyDivergence, SchemaVersion: "1.0.0", Payload: payload})
	if err != nil {
		t.Fatalf("DecodeConfigKeyDivergence error = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, want)
	}
}

// TestDecodeRefOverlayKinds_MissingRequiredFieldDeadLetters proves a payload
// missing any required key fails closed with a classified *DecodeError
// naming exactly that field — never a zero-value struct.
func TestDecodeRefOverlayKinds_MissingRequiredFieldDeadLetters(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind    string
		decode  func(Envelope) error
		payload map[string]any
		missing string
	}{
		{
			kind: FactKindRefOverlayBranchObservation,
			decode: func(env Envelope) error {
				_, err := DecodeRefOverlayBranchObservation(env)
				return err
			},
			payload: map[string]any{
				"ref":           "env/prod",
				"ref_kind":      refOverlayV1.RefKindBranch,
				"resolvability": refOverlayV1.ResolvabilityMobile,
			},
			missing: "repo_id",
		},
		{
			kind: FactKindConfigKeyDivergence,
			decode: func(env Envelope) error {
				_, err := DecodeConfigKeyDivergence(env)
				return err
			},
			payload: map[string]any{
				"repo_id":    "github.com/eshu-hq/eshu",
				"ref":        "env/prod",
				"config_key": "service.api_endpoint",
				"env":        "prod",
			},
			missing: "diverged",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.kind+"/"+tc.missing, func(t *testing.T) {
			t.Parallel()
			err := tc.decode(Envelope{FactKind: tc.kind, SchemaVersion: "1.0.0", Payload: tc.payload})
			if err == nil {
				t.Fatalf("decode %s missing %q: error = nil, want *DecodeError", tc.kind, tc.missing)
			}
			decodeErr, ok := err.(*DecodeError)
			if !ok {
				t.Fatalf("decode %s missing %q: error type %T, want *DecodeError", tc.kind, tc.missing, err)
			}
			if decodeErr.Field != tc.missing {
				t.Fatalf("decode %s missing %q: DecodeError.Field = %q, want %q", tc.kind, tc.missing, decodeErr.Field, tc.missing)
			}
			if decodeErr.Classification != ClassificationInputInvalid {
				t.Fatalf("decode %s missing %q: classification = %q, want %q", tc.kind, tc.missing, decodeErr.Classification, ClassificationInputInvalid)
			}
		})
	}
}
