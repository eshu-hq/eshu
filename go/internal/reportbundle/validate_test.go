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

// TestValidationChecksMatchValidateBehavior guards the P2 from the #5060 review:
// Bundle.Validation.Checks must not drift from Validate's actual checks. It
// asserts Capture records exactly ValidationChecks, that a bundle satisfying
// every listed check passes Validate (each listed name maps to a real gate,
// not a no-op), and that each listed check rejects when its precondition is
// violated. The len(cases)==len(ValidationChecks) guard forces a new entry to
// arrive with its own rejection case; it cannot prove the absence of an
// unlisted always-run check, so adding a check to Validate still requires
// adding its name here by hand (the doc contract on ValidationChecks).
func TestValidationChecksMatchValidateBehavior(t *testing.T) {
	valid := minimalPublicBundle(t)

	if len(valid.Validation.Checks) != len(ValidationChecks) {
		t.Fatalf("Capture recorded %d checks, want %d (ValidationChecks)", len(valid.Validation.Checks), len(ValidationChecks))
	}
	for i, name := range ValidationChecks {
		if valid.Validation.Checks[i] != name {
			t.Fatalf("Capture recorded Checks[%d]=%q, want %q from ValidationChecks", i, valid.Validation.Checks[i], name)
		}
	}
	if err := Validate(valid, ValidateOptions{}); err != nil {
		t.Fatalf("valid bundle failed Validate: %v (an always-run check may be missing from ValidationChecks)", err)
	}

	cases := []struct {
		check  string
		mutate func(*Bundle)
	}{
		{"schema_version", func(b *Bundle) { b.SchemaVersion = "report/vX" }},
		{"bundle_id", func(b *Bundle) { b.BundleID = "" }},
		{"profile_payloads_consistency", func(b *Bundle) { b.Payloads = &PayloadAttachment{Warning: "x"} }},
		{"share_safe_keys", func(b *Bundle) { b.Query.Params = map[string]any{"api_key": "leak"} }},
	}
	for _, tc := range cases {
		found := false
		for _, n := range ValidationChecks {
			if n == tc.check {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("check %q is exercised here but absent from ValidationChecks", tc.check)
		}
		b := minimalPublicBundle(t)
		tc.mutate(&b)
		if err := Validate(b, ValidateOptions{}); err == nil {
			t.Fatalf("Validate accepted a bundle violating %q, want rejection", tc.check)
		}
	}

	if len(ValidationChecks) != len(cases) {
		t.Fatalf("ValidationChecks has %d entries but %d are behavior-guarded here — add the new check's rejection case when extending Validate", len(ValidationChecks), len(cases))
	}
}
