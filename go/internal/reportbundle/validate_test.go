// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestValidate_PublicProfileWithPayloadsIsRejected is the redaction-safety
// regression that matters most: a bundle can claim redaction.profile ==
// public yet still carry a non-nil Payloads section (raw excerpts / fact
// payload bytes). Validate MUST reject that inconsistency instead of silently
// stripping Payloads before the share-safe walk and passing — otherwise a
// maintainer's `eshu report validate --require-public` gate green-lights a
// bundle that leaks private payload bytes. Payloads are legitimate ONLY under
// the private-triage profile.
func TestValidate_PublicProfileWithPayloadsIsRejected(t *testing.T) {
	t.Parallel()

	tamperedPublic := func(t *testing.T) Bundle {
		t.Helper()
		bundle := minimalPublicBundle(t)
		// Profile stays public, but a payload attachment is smuggled in.
		bundle.Payloads = &PayloadAttachment{
			Warning: payloadAttachmentWarning,
			Excerpts: []CitationExcerpt{
				{
					CitationRef: CitationRef{Kind: "file", RepoID: "demo/service", RelativePath: "main.go"},
					Excerpt:     "func Handler() { secretValue }",
				},
			},
			Facts: []facts.Envelope{
				{FactID: "f1", FactKind: "repository", StableFactKey: "repo:demo/service", ScopeID: "s1", GenerationID: "g1", Payload: map[string]any{"description": "leaked"}},
			},
		}
		return bundle
	}

	tests := []struct {
		name string
		opts ValidateOptions
	}{
		{name: "require-public rejects public+payloads", opts: ValidateOptions{RequirePublic: true}},
		{name: "default validate also rejects public+payloads", opts: ValidateOptions{}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bundle := tamperedPublic(t)
			err := Validate(bundle, tt.opts)
			if err == nil {
				t.Fatalf("Validate(public profile + non-nil Payloads, %+v) error = nil, want rejection", tt.opts)
			}
			if !strings.Contains(err.Error(), "payload") {
				t.Fatalf("Validate() error = %q, want a payload-inconsistency message", err.Error())
			}
		})
	}
}

// TestValidate_PrivateTriageWithPayloadsPasses proves the legitimate case
// still works: a private-triage bundle that carries a Payloads attachment
// passes default Validate (the payload section is excluded from the share-safe
// walk, the rest of the bundle is still checked), but fails --require-public.
func TestValidate_PrivateTriageWithPayloadsPasses(t *testing.T) {
	t.Parallel()

	bundle, err := Capture(CaptureInput{
		Surface:         "api",
		Target:          "/api/v0/services/checkout/story",
		IncludePayloads: true,
		PayloadExcerpts: []CitationExcerpt{
			{CitationRef: CitationRef{Kind: "file", RepoID: "demo/service", RelativePath: "main.go"}, Excerpt: "raw excerpt"},
		},
	})
	if err != nil {
		t.Fatalf("Capture() error = %v, want nil", err)
	}
	if bundle.Redaction.Profile != ProfilePrivateTriage || bundle.Payloads == nil {
		t.Fatalf("expected a private-triage bundle with a payload attachment, got profile=%q payloads=%v", bundle.Redaction.Profile, bundle.Payloads)
	}
	if err := Validate(bundle, ValidateOptions{}); err != nil {
		t.Fatalf("Validate(private-triage bundle) error = %v, want nil", err)
	}
	if err := Validate(bundle, ValidateOptions{RequirePublic: true}); err == nil {
		t.Fatalf("Validate(private-triage bundle, RequirePublic) error = nil, want rejection")
	}
}

// TestValidate_PrivateTriageWithoutPayloadsIsRejected guards the inverse
// inconsistency: a bundle labeled private-triage but carrying no Payloads
// attachment is malformed (the profile only exists to carry payloads), and
// Validate rejects it rather than accept a mislabeled artifact.
func TestValidate_PrivateTriageWithoutPayloadsIsRejected(t *testing.T) {
	t.Parallel()

	bundle := minimalPublicBundle(t)
	bundle.Redaction.Profile = ProfilePrivateTriage
	bundle.Payloads = nil

	if err := Validate(bundle, ValidateOptions{}); err == nil {
		t.Fatalf("Validate(private-triage profile + nil Payloads) error = nil, want rejection")
	}
}
