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
				if !strings.Contains(err.Error(), fieldCase.field) {
					t.Fatalf("LoadManifest() error = %v, want it to name %s", err, fieldCase.field)
				}
			})
		}
	}
}

// TestPolicyNeverSeesPaddedManifestSoRevokeCannotMissIt pins the failure the
// padding check closes: before it, a manifest with a padded ID loaded, an
// unpadded revoke entry compared with == did not match it, and the verdict
// moved on to incompatible_core instead of revoked_package. The manifest must
// now be refused as invalid_manifest before any revoke comparison runs, for
// the ID and the publisher.
func TestPolicyNeverSeesPaddedManifestSoRevokeCannotMissIt(t *testing.T) {
	t.Parallel()

	policy := Policy{
		Mode:              TrustModeAllowlist,
		AllowedIDs:        []string{"dev.eshu.collector.aws"},
		AllowedPublishers: []string{"eshu-hq"},
		RevokedIDs:        []string{"dev.eshu.collector.aws"},
		RevokedPublishers: []string{"eshu-hq"},
		CoreVersion:       "v0.0.5",
	}

	for _, fieldCase := range paddedIdentityFields {
		t.Run(fieldCase.field, func(t *testing.T) {
			t.Parallel()

			manifest := validManifest()
			fieldCase.set(&manifest, " "+fieldCase.valid+" ")

			result := policy.Verify(manifest)
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
