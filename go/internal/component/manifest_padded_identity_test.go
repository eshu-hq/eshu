// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"fmt"
	"strings"
	"testing"
)

// paddedIdentityCases lists the padding shapes an identity field must refuse.
// Each value is a valid identifier once trimmed, so a check that trims before
// matching the pattern would accept every one.
var paddedIdentityCases = []struct {
	name   string
	prefix string
	suffix string
}{
	{name: "leading space", prefix: " "},
	{name: "trailing space", suffix: " "},
	{name: "leading and trailing space", prefix: " ", suffix: " "},
	{name: "leading tab", prefix: "\t"},
	{name: "trailing newline", suffix: "\n"},
	// strings.TrimSpace strips Unicode White_Space, so these were accepted
	// silently before the check and are rejected with the same message now.
	{name: "leading no-break space", prefix: " "},
	{name: "trailing no-break space", suffix: " "},
	{name: "leading em space", prefix: " "},
	{name: "trailing em space", suffix: " "},
}

// paddedIdentityFields maps each identity field to the manifest mutation and
// the YAML line that carry it, so struct and file loading share one table.
var paddedIdentityFields = []struct {
	field    string
	yamlLine string
	valid    string
	set      func(*Manifest, string)
}{
	{
		field:    "metadata.id",
		yamlLine: "  id: ",
		valid:    "dev.eshu.collector.aws",
		set:      func(m *Manifest, v string) { m.Metadata.ID = v },
	},
	{
		field:    "metadata.publisher",
		yamlLine: "  publisher: ",
		valid:    "eshu-hq",
		set:      func(m *Manifest, v string) { m.Metadata.Publisher = v },
	},
	{
		// validateIdentifier is shared, so a collector kind is held to the
		// same canonical rule at manifest load.
		field:    "collector kind",
		yamlLine: "    - ",
		valid:    "aws",
		set:      func(m *Manifest, v string) { m.Spec.CollectorKinds[0] = v },
	},
}

func TestManifestValidateRejectsPaddedIdentity(t *testing.T) {
	t.Parallel()

	for _, fieldCase := range paddedIdentityFields {
		for _, padCase := range paddedIdentityCases {
			t.Run(fieldCase.field+"/"+padCase.name, func(t *testing.T) {
				t.Parallel()

				manifest := validManifest()
				fieldCase.set(&manifest, padCase.prefix+fieldCase.valid+padCase.suffix)

				err := manifest.Validate()
				if err == nil {
					t.Fatalf("Validate() error = nil, want %s padding error", fieldCase.field)
				}
				if !strings.Contains(err.Error(), fieldCase.field) {
					t.Fatalf("Validate() error = %v, want it to name %s", err, fieldCase.field)
				}
				if !strings.Contains(err.Error(), "surrounding whitespace") {
					t.Fatalf("Validate() error = %v, want it to explain surrounding whitespace", err)
				}
			})
		}
	}
}

func TestLoadManifestRejectsPaddedIdentity(t *testing.T) {
	t.Parallel()

	for _, fieldCase := range paddedIdentityFields {
		for _, padCase := range paddedIdentityCases {
			t.Run(fieldCase.field+"/"+padCase.name, func(t *testing.T) {
				t.Parallel()

				// %q escapes the tab and newline into a YAML double-quoted
				// scalar that decodes back to the padded bytes.
				padded := fmt.Sprintf("%q", padCase.prefix+fieldCase.valid+padCase.suffix)
				body := strings.Replace(
					validManifestYAML(),
					fieldCase.yamlLine+fieldCase.valid,
					fieldCase.yamlLine+padded,
					1,
				)
				if body == validManifestYAML() {
					t.Fatalf("fixture did not contain %q", fieldCase.yamlLine+fieldCase.valid)
				}

				_, err := LoadManifest(writeManifest(t, body))
				if err == nil {
					t.Fatalf("LoadManifest() error = nil, want %s padding error", fieldCase.field)
				}
				if got := ErrorCodeOf(err); got != ErrorCodeInvalidManifest {
					t.Fatalf("LoadManifest() code = %q, want %q; err=%v", got, ErrorCodeInvalidManifest, err)
				}
				if !strings.Contains(err.Error(), fieldCase.field) ||
					!strings.Contains(err.Error(), "must be canonical without surrounding whitespace") {
					t.Fatalf("LoadManifest() error = %v, want the canonical-form message for %s", err, fieldCase.field)
				}
			})
		}
	}
}

// TestPolicyNeverSeesPaddedManifestSoRevokeCannotMissIt replays the issue's
// exact symptom. Each row revokes only the field it pads: a revoke-only-ID
// policy for the metadata.id row and a revoke-only-publisher policy for the
// publisher row. The core version sits outside the manifest's compatibleCore
// range. On the base, the padded manifest loaded, the unpadded revoke entry
// compared with == did not match it, and the verdict fell through to
// incompatible_core instead of revoked_package. The manifest must now be
// refused as invalid_manifest before any revoke comparison runs. The base
// behavior is stated here, not exercised at test time.
func TestPolicyNeverSeesPaddedManifestSoRevokeCannotMissIt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		fieldIndex int
		policy     Policy
	}{
		{
			fieldIndex: 0,
			policy: Policy{
				Mode:        TrustModeAllowlist,
				RevokedIDs:  []string{"dev.eshu.collector.aws"},
				CoreVersion: "v0.2.0",
			},
		},
		{
			fieldIndex: 1,
			policy: Policy{
				Mode:              TrustModeAllowlist,
				RevokedPublishers: []string{"eshu-hq"},
				CoreVersion:       "v0.2.0",
			},
		},
	}

	for _, tc := range cases {
		fieldCase := paddedIdentityFields[tc.fieldIndex]
		t.Run(fieldCase.field, func(t *testing.T) {
			t.Parallel()

			unpadded := tc.policy.Verify(validManifest())
			if got, want := unpadded.Code, ErrorCodeRevokedPackage; got != want {
				t.Fatalf("control Verify().Code = %q, want %q for the unpadded manifest; reason = %q", got, want, unpadded.Reason)
			}

			manifest := validManifest()
			fieldCase.set(&manifest, " "+fieldCase.valid+" ")

			result := tc.policy.Verify(manifest)
			if result.Allowed {
				t.Fatalf("Verify().Allowed = true for a padded %s, want false", fieldCase.field)
			}
			if got, want := result.Code, ErrorCodeInvalidManifest; got != want {
				t.Fatalf("Verify().Code = %q, want %q; reason = %q", got, want, result.Reason)
			}
			if !strings.Contains(result.Reason, fieldCase.field) {
				t.Fatalf("Verify().Reason = %q, want it to name %s", result.Reason, fieldCase.field)
			}
		})
	}
}

// TestLoadManifestRejectsBlockScalarIdentityNewline pins the YAML shape that
// pads without visible whitespace: a literal or folded block scalar keeps a
// trailing newline, which is padding. The strip chomping indicator removes it,
// so that spelling stays valid.
func TestLoadManifestRejectsBlockScalarIdentityNewline(t *testing.T) {
	t.Parallel()

	for _, indicator := range []string{"|", ">"} {
		for _, fieldCase := range paddedIdentityFields[:2] {
			t.Run(fieldCase.field+"/"+indicator, func(t *testing.T) {
				t.Parallel()

				body := strings.Replace(
					validManifestYAML(),
					fieldCase.yamlLine+fieldCase.valid,
					fieldCase.yamlLine+indicator+"\n    "+fieldCase.valid,
					1,
				)
				_, err := LoadManifest(writeManifest(t, body))
				if err == nil {
					t.Fatalf("LoadManifest() error = nil, want %s newline padding error", fieldCase.field)
				}
				if got := ErrorCodeOf(err); got != ErrorCodeInvalidManifest {
					t.Fatalf("LoadManifest() code = %q, want %q; err=%v", got, ErrorCodeInvalidManifest, err)
				}
				if !strings.Contains(err.Error(), fieldCase.field+` "`) ||
					!strings.Contains(err.Error(), "must be canonical without surrounding whitespace") {
					t.Fatalf("LoadManifest() error = %v, want the canonical-form message for %s", err, fieldCase.field)
				}
			})
		}
	}

	stripped := strings.Replace(
		validManifestYAML(),
		"  id: dev.eshu.collector.aws",
		"  id: |-\n    dev.eshu.collector.aws",
		1,
	)
	if _, err := LoadManifest(writeManifest(t, stripped)); err != nil {
		t.Fatalf("LoadManifest() error = %v, want nil for a |- block scalar with no trailing newline", err)
	}
}

func TestManifestValidateStillAcceptsUnpaddedIdentity(t *testing.T) {
	t.Parallel()

	if err := validManifest().Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil for an unpadded manifest", err)
	}
	result := Policy{
		Mode:        TrustModeAllowlist,
		RevokedIDs:  []string{"dev.eshu.collector.aws"},
		CoreVersion: "v0.0.5",
	}.Verify(validManifest())
	if got, want := result.Code, ErrorCodeRevokedPackage; got != want {
		t.Fatalf("Verify().Code = %q, want %q for the unpadded revoked ID", got, want)
	}
}
