// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractTerraformStateRowsQuarantinesMissingResourceAddress is the
// flagship regression for the projector's terraform_state typed-decode
// migration (Contract System v1 §3.2, Option B per-fact quarantine). It
// proves the accuracy guarantee AND the per-fact isolation contract: a
// terraform_state_resource fact missing its required address key is
// QUARANTINED as a visible input_invalid dead-letter — never silently
// producing a resource row under a graph identity built from an empty-string
// address segment — while every VALID fact in the same batch still projects,
// and the whole-repo canonical build never fails.
//
// Before the migration this behavior was impossible: terraformStateResourceRow
// read address with payloadString, which returns "" for the absent key, and
// the row was dropped with no operator signal (a silent skip). A collector
// regression dropping address produced zero resources and no dead-letter.
func TestExtractTerraformStateRowsQuarantinesMissingResourceAddress(t *testing.T) {
	t.Parallel()

	validFacts := terraformStateFacts()

	// A resource fact whose required address key is ABSENT (not merely empty).
	malformed := facts.Envelope{
		FactID:        "tf-resource-bad",
		ScopeID:       "tf-scope-1",
		GenerationID:  "tf-generation-1",
		FactKind:      facts.TerraformStateResourceFactKind,
		SchemaVersion: facts.TerraformStateResourceSchemaVersion,
		Payload: map[string]any{
			// "address" intentionally absent.
			"type": "aws_instance",
		},
	}

	mat := &CanonicalMaterialization{ScopeID: "tf-scope-1"}
	quarantined := extractTerraformStateRows(mat, append(validFacts, malformed))

	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1; the missing-address resource fact must be quarantined", len(quarantined))
	}
	if got := quarantined[0].factKind; got != facts.TerraformStateResourceFactKind {
		t.Fatalf("quarantined fact kind = %q, want %q", got, facts.TerraformStateResourceFactKind)
	}
	if got := quarantined[0].field; got != "address" {
		t.Fatalf("quarantined field = %q, want %q", got, "address")
	}
	if got := quarantined[0].factID; got != "tf-resource-bad" {
		t.Fatalf("quarantined fact id = %q, want %q", got, "tf-resource-bad")
	}

	// The batch's VALID resource must still materialize: isolation means a
	// poisoned sibling never suppresses valid graph truth.
	if len(mat.TerraformStateResources) != 1 {
		t.Fatalf("len(TerraformStateResources) = %d, want 1; the valid resource must still project despite the quarantined fact", len(mat.TerraformStateResources))
	}
	if got, want := mat.TerraformStateResources[0].Address, "module.app.aws_instance.web"; got != want {
		t.Fatalf("valid resource Address = %q, want %q", got, want)
	}
}

// TestExtractTerraformStateRowsPresentButEmptyAddressIsDroppedNotQuarantined
// proves the absent-vs-present-empty distinction: a resource fact whose
// address key is PRESENT but empty is a valid decode (not a quarantine) that
// is still dropped as an incomplete, non-materializable row — byte-identical
// to the pre-typing behavior, where payloadString("") produced no row. Only an
// ABSENT (or null) required key dead-letters.
func TestExtractTerraformStateRowsPresentButEmptyAddressIsDroppedNotQuarantined(t *testing.T) {
	t.Parallel()

	emptyAddress := facts.Envelope{
		FactID:        "tf-resource-empty",
		ScopeID:       "tf-scope-1",
		GenerationID:  "tf-generation-1",
		FactKind:      facts.TerraformStateResourceFactKind,
		SchemaVersion: facts.TerraformStateResourceSchemaVersion,
		Payload: map[string]any{
			"address": "", // present but empty
			"type":    "aws_instance",
		},
	}

	mat := &CanonicalMaterialization{ScopeID: "tf-scope-1"}
	quarantined := extractTerraformStateRows(mat, []facts.Envelope{emptyAddress})

	if len(quarantined) != 0 {
		t.Fatalf("len(quarantined) = %d, want 0; a present-but-empty required field is a valid decode, not a quarantine", len(quarantined))
	}
	if len(mat.TerraformStateResources) != 0 {
		t.Fatalf("len(TerraformStateResources) = %d, want 0; a resource with an empty address is incomplete and must be dropped", len(mat.TerraformStateResources))
	}
}

// TestExtractTerraformStateRowsWhitespaceAddressIsDroppedNotMaterialized
// proves the trim-before-gate accuracy contract (the k8s/oci-family review
// lesson): the pre-typing payloadString path TRIMMED whitespace before
// deciding whether an identity was usable, so a whitespace-only address
// ("   ") was treated as empty and the row was DROPPED. The typed row gate
// must preserve that: it trims the identity field before the `== ""` check,
// so a present-but-whitespace-only address drops the row as non-materializable
// rather than keying a resource row on an empty-after-trim graph identity.
// This is NOT a dead-letter — the decode succeeded, the fact is a valid but
// non-materializable observation, exactly like present-but-empty.
func TestExtractTerraformStateRowsWhitespaceAddressIsDroppedNotMaterialized(t *testing.T) {
	t.Parallel()

	validFacts := terraformStateFacts()

	whitespaceAddress := facts.Envelope{
		FactID:        "tf-resource-ws",
		ScopeID:       "tf-scope-1",
		GenerationID:  "tf-generation-1",
		FactKind:      facts.TerraformStateResourceFactKind,
		SchemaVersion: facts.TerraformStateResourceSchemaVersion,
		Payload: map[string]any{
			"address": "   ", // present but whitespace-only
			"type":    "aws_instance",
		},
	}

	mat := &CanonicalMaterialization{ScopeID: "tf-scope-1"}
	quarantined := extractTerraformStateRows(mat, append(validFacts, whitespaceAddress))

	// A whitespace-only identity is a valid decode, so it must NOT dead-letter.
	if len(quarantined) != 0 {
		t.Fatalf("len(quarantined) = %d, want 0; a whitespace-only identity is a valid decode dropped as non-materializable, never a dead-letter", len(quarantined))
	}
	// Only the valid resource from terraformStateFacts() may materialize.
	if len(mat.TerraformStateResources) != 1 {
		t.Fatalf("len(TerraformStateResources) = %d, want 1; the whitespace-address resource must be dropped, the valid sibling must still project", len(mat.TerraformStateResources))
	}
	if got, want := mat.TerraformStateResources[0].Address, "module.app.aws_instance.web"; got != want {
		t.Fatalf("materialized resource Address = %q, want the valid sibling's %q (never a whitespace-only identity)", got, want)
	}
}

// TestExtractTerraformStateRowsQuarantinesMissingTagObservationJoinKey proves
// the tag-observation join-key quarantine contract: a
// terraform_state_tag_observation fact missing its required resource_address
// join key is quarantined, and does not silently break the tag->resource join
// for the rest of the batch.
func TestExtractTerraformStateRowsQuarantinesMissingTagObservationJoinKey(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:        "tf-tag-bad",
		ScopeID:       "tf-scope-1",
		GenerationID:  "tf-generation-1",
		FactKind:      facts.TerraformStateTagObservationFactKind,
		SchemaVersion: facts.TerraformStateTagObservationSchemaVersion,
		Payload: map[string]any{
			// "resource_address" intentionally absent.
			"tag_key_hash": "tag-key-hash-bad",
		},
	}

	mat := &CanonicalMaterialization{ScopeID: "tf-scope-1"}
	quarantined := extractTerraformStateRows(mat, append(terraformStateFacts(), malformed))

	var found bool
	for _, q := range quarantined {
		if q.factID == "tf-tag-bad" {
			found = true
			if q.factKind != facts.TerraformStateTagObservationFactKind {
				t.Fatalf("quarantined fact kind = %q, want %q", q.factKind, facts.TerraformStateTagObservationFactKind)
			}
			if q.field != "resource_address" {
				t.Fatalf("quarantined field = %q, want %q", q.field, "resource_address")
			}
		}
	}
	if !found {
		t.Fatal("tf-tag-bad was not quarantined; a missing resource_address join key must dead-letter as input_invalid")
	}

	// The valid resource's tag hash from terraformStateFacts() must still join.
	if len(mat.TerraformStateResources) != 1 {
		t.Fatalf("len(TerraformStateResources) = %d, want 1", len(mat.TerraformStateResources))
	}
	if len(mat.TerraformStateResources[0].TagKeyHashes) != 1 {
		t.Fatalf("len(TagKeyHashes) = %d, want 1; the valid tag observation must still join despite the quarantined sibling", len(mat.TerraformStateResources[0].TagKeyHashes))
	}
}

// TestExtractTerraformStateRowsQuarantinesMissingProviderBindingJoinKey is the
// #5446 input_invalid regression for the new provider-binding pre-pass: a
// terraform_state_provider_binding fact missing its required
// resource_address join key is quarantined, and does not silently break the
// provider->resource join for the rest of the batch.
func TestExtractTerraformStateRowsQuarantinesMissingProviderBindingJoinKey(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:        "tf-provider-binding-bad",
		ScopeID:       "tf-scope-1",
		GenerationID:  "tf-generation-1",
		FactKind:      facts.TerraformStateProviderBindingFactKind,
		SchemaVersion: facts.TerraformStateProviderBindingSchemaVersion,
		Payload: map[string]any{
			// "resource_address" intentionally absent.
			"provider_address": "provider[\"registry.terraform.io/hashicorp/aws\"]",
		},
	}

	mat := &CanonicalMaterialization{ScopeID: "tf-scope-1"}
	quarantined := extractTerraformStateRows(mat, append(terraformStateFacts(), malformed))

	var found bool
	for _, q := range quarantined {
		if q.factID == "tf-provider-binding-bad" {
			found = true
			if q.factKind != facts.TerraformStateProviderBindingFactKind {
				t.Fatalf("quarantined fact kind = %q, want %q", q.factKind, facts.TerraformStateProviderBindingFactKind)
			}
			if q.field != "resource_address" {
				t.Fatalf("quarantined field = %q, want %q", q.field, "resource_address")
			}
		}
	}
	if !found {
		t.Fatal("tf-provider-binding-bad was not quarantined; a missing resource_address join key must dead-letter as input_invalid")
	}

	// The valid resource from terraformStateFacts() must still project,
	// simply without a provider binding joined (no valid binding fact for it
	// in this batch).
	if len(mat.TerraformStateResources) != 1 {
		t.Fatalf("len(TerraformStateResources) = %d, want 1; a poisoned provider_binding sibling must never suppress valid resource projection", len(mat.TerraformStateResources))
	}
}

// TestExtractTerraformStateRowsProviderBindingDuplicateAddressFirstWins
// proves the pre-pass's documented "first valid binding wins" policy for a
// resource address seen in more than one terraform_state_provider_binding
// fact (an anomaly — Terraform state always binds a resource to exactly one
// provider configuration — but the pre-pass must still behave
// deterministically rather than silently reordering under map iteration).
func TestExtractTerraformStateRowsProviderBindingDuplicateAddressFirstWins(t *testing.T) {
	t.Parallel()

	address := "module.app.aws_instance.web"
	first := facts.Envelope{
		FactID:        "tf-provider-binding-first",
		ScopeID:       "tf-scope-1",
		GenerationID:  "tf-generation-1",
		FactKind:      facts.TerraformStateProviderBindingFactKind,
		SchemaVersion: facts.TerraformStateProviderBindingSchemaVersion,
		Payload: map[string]any{
			"resource_address": address,
			"provider_address": "provider[\"registry.terraform.io/hashicorp/aws\"]",
			"provider_type":    "aws",
		},
	}
	duplicate := facts.Envelope{
		FactID:        "tf-provider-binding-duplicate",
		ScopeID:       "tf-scope-1",
		GenerationID:  "tf-generation-1",
		FactKind:      facts.TerraformStateProviderBindingFactKind,
		SchemaVersion: facts.TerraformStateProviderBindingSchemaVersion,
		Payload: map[string]any{
			"resource_address": address,
			"provider_address": "provider[\"registry.terraform.io/hashicorp/google\"]",
			"provider_type":    "google",
		},
	}

	mat := &CanonicalMaterialization{ScopeID: "tf-scope-1"}
	quarantined := extractTerraformStateRows(mat, append(terraformStateFacts(), first, duplicate))

	if len(quarantined) != 0 {
		t.Fatalf("len(quarantined) = %d, want 0; a duplicate provider binding for the same resource is dropped, not quarantined", len(quarantined))
	}
	if len(mat.TerraformStateResources) != 1 {
		t.Fatalf("len(TerraformStateResources) = %d, want 1", len(mat.TerraformStateResources))
	}
	if got, want := mat.TerraformStateResources[0].Provider, "aws"; got != want {
		t.Fatalf("resource.Provider = %q, want %q (first valid binding must win over the duplicate)", got, want)
	}
}
