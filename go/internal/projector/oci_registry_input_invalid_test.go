// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractOCIRegistryRowsQuarantinesMissingManifestDigest is the flagship
// regression for the projector's typed-decode migration (Contract System v1
// §3.2, Option B per-fact quarantine). It proves the accuracy guarantee AND the
// per-fact isolation contract: an oci_registry.image_manifest fact missing its
// required digest key is QUARANTINED as a visible input_invalid dead-letter —
// never silently producing a manifest row under a descriptor UID built from an
// empty-string digest segment — while every VALID fact in the same batch still
// projects, and the whole-repo canonical build never fails.
//
// Before the migration this behavior was impossible: ociImageManifestRow read
// digest with payloadString, which returns "" for the absent key, and the row
// was dropped with no operator signal (a silent skip). A collector regression
// dropping digest produced zero manifests and no dead-letter.
//
// After the migration extractOCIRegistryRows decodes each oci fact through
// factschema.DecodeOCIImageManifest; the malformed fact yields a classified
// *factschema.DecodeError that partitionProjectorDecodeFailures routes to a per-fact
// quarantine recorded on the materialization. The valid manifest still
// projects.
func TestExtractOCIRegistryRowsQuarantinesMissingManifestDigest(t *testing.T) {
	t.Parallel()

	validManifest := ociRegistryFacts()[2] // the oci-manifest-1 fact (fully valid)

	// A manifest fact whose required digest key is ABSENT (not merely empty):
	// the exact malformed input the accuracy guarantee names. repository_id is
	// present so the ONLY reason to quarantine is the missing digest.
	malformed := facts.Envelope{
		FactID:        "oci-manifest-bad",
		ScopeID:       "oci-scope-1",
		GenerationID:  "oci-generation-1",
		FactKind:      facts.OCIImageManifestFactKind,
		SchemaVersion: facts.OCIImageManifestSchemaVersion,
		Payload: map[string]any{
			"repository_id": "oci-registry://registry.example.com/team/api",
			// "digest" intentionally absent.
			"media_type": "application/vnd.oci.image.manifest.v1+json",
		},
	}

	mat := &CanonicalMaterialization{}
	quarantined := extractOCIRegistryRows(mat, []facts.Envelope{validManifest, malformed})

	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1; the missing-digest manifest fact must be quarantined", len(quarantined))
	}
	if got := quarantined[0].factKind; got != facts.OCIImageManifestFactKind {
		t.Fatalf("quarantined fact kind = %q, want %q", got, facts.OCIImageManifestFactKind)
	}
	if got := quarantined[0].field; got != "digest" {
		t.Fatalf("quarantined field = %q, want %q", got, "digest")
	}
	if got := quarantined[0].factID; got != "oci-manifest-bad" {
		t.Fatalf("quarantined fact id = %q, want %q", got, "oci-manifest-bad")
	}

	// The batch's VALID manifest must still materialize: isolation means a
	// poisoned sibling never suppresses valid graph truth.
	if len(mat.OCIImageManifests) != 1 {
		t.Fatalf("len(OCIImageManifests) = %d, want 1; the valid manifest must still project despite the quarantined fact", len(mat.OCIImageManifests))
	}
	if got, want := mat.OCIImageManifests[0].UID, ociManifestDescriptorID(); got != want {
		t.Fatalf("valid manifest UID = %q, want %q", got, want)
	}
}

// TestExtractOCIRegistryRowsPresentButEmptyDigestIsDroppedNotQuarantined proves
// the absent-vs-present-empty distinction: a manifest fact whose digest key is
// PRESENT but empty is a valid decode (not a quarantine) that is still dropped
// as an incomplete, non-materializable row — byte-identical to the pre-typing
// behavior, where payloadString("") produced no row. Only an ABSENT (or null)
// required key dead-letters.
func TestExtractOCIRegistryRowsPresentButEmptyDigestIsDroppedNotQuarantined(t *testing.T) {
	t.Parallel()

	emptyDigest := facts.Envelope{
		FactID:        "oci-manifest-empty",
		ScopeID:       "oci-scope-1",
		GenerationID:  "oci-generation-1",
		FactKind:      facts.OCIImageManifestFactKind,
		SchemaVersion: facts.OCIImageManifestSchemaVersion,
		Payload: map[string]any{
			"repository_id": "oci-registry://registry.example.com/team/api",
			"digest":        "", // present but empty
			"media_type":    "application/vnd.oci.image.manifest.v1+json",
		},
	}

	mat := &CanonicalMaterialization{}
	quarantined := extractOCIRegistryRows(mat, []facts.Envelope{emptyDigest})

	if len(quarantined) != 0 {
		t.Fatalf("len(quarantined) = %d, want 0; a present-but-empty required field is a valid decode, not a quarantine", len(quarantined))
	}
	if len(mat.OCIImageManifests) != 0 {
		t.Fatalf("len(OCIImageManifests) = %d, want 0; a manifest with an empty digest is incomplete and must be dropped", len(mat.OCIImageManifests))
	}
}

// TestExtractOCIRegistryRowsWhitespaceDigestIsDroppedNotMaterialized is the
// regression for codex's P2 (PR #4699, oci_registry_canonical.go:253): the
// pre-typing payloadString path TRIMMED whitespace before deciding whether an
// identity was usable, so a whitespace-only digest ("   ") was treated as empty
// and the row was DROPPED. The typed row gate must preserve that: it trims the
// identity fields before the `== ""` check, so a present-but-whitespace-only
// digest drops the row as non-materializable rather than keying a descriptor row
// on an empty-after-trim graph identity (the degenerate identity the
// downstream ociDescriptorUID/ociResolvedDescriptorUID trim would otherwise
// produce). This is NOT a dead-letter — the decode succeeded, the fact is a
// valid but non-materializable observation, exactly like present-but-empty.
//
// Before the trim fix this test failed: the raw `manifest.Digest == ""` gate let
// "   " through, and the manifest materialized under an empty-digest uid. After
// the fix it is dropped, while a valid sibling manifest in the same batch still
// materializes.
func TestExtractOCIRegistryRowsWhitespaceDigestIsDroppedNotMaterialized(t *testing.T) {
	t.Parallel()

	validManifest := ociRegistryFacts()[2] // the fully valid oci-manifest-1 fact

	whitespaceDigest := facts.Envelope{
		FactID:        "oci-manifest-ws",
		ScopeID:       "oci-scope-1",
		GenerationID:  "oci-generation-1",
		FactKind:      facts.OCIImageManifestFactKind,
		SchemaVersion: facts.OCIImageManifestSchemaVersion,
		Payload: map[string]any{
			"repository_id": "oci-registry://registry.example.com/team/api",
			"digest":        "   ", // present but whitespace-only
			"media_type":    "application/vnd.oci.image.manifest.v1+json",
		},
	}

	mat := &CanonicalMaterialization{}
	quarantined := extractOCIRegistryRows(mat, []facts.Envelope{validManifest, whitespaceDigest})

	// A whitespace-only identity is a valid decode, so it must NOT dead-letter.
	if len(quarantined) != 0 {
		t.Fatalf("len(quarantined) = %d, want 0; a whitespace-only identity is a valid decode dropped as non-materializable, never a dead-letter", len(quarantined))
	}
	// Only the valid manifest may materialize — the whitespace-digest fact must
	// never key a row on an empty-after-trim graph identity.
	if len(mat.OCIImageManifests) != 1 {
		t.Fatalf("len(OCIImageManifests) = %d, want 1; the whitespace-only-digest manifest must be dropped, the valid sibling must still project", len(mat.OCIImageManifests))
	}
	if got, want := mat.OCIImageManifests[0].UID, ociManifestDescriptorID(); got != want {
		t.Fatalf("materialized manifest UID = %q, want the valid sibling's %q (never an empty-digest identity)", got, want)
	}
}
