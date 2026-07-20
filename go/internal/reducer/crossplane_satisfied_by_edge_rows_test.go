// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// crossplaneContentEntityEnvelope builds a minimal content_entity fact
// envelope for the given entity_type and entity_metadata, mirroring the
// shape internal/collector/git_content_fact_envelopes.go's
// contentEntityFactEnvelope emits.
func crossplaneContentEntityEnvelope(entityID, entityType string, metadata map[string]any) facts.Envelope {
	return facts.Envelope{
		FactID:   entityID,
		FactKind: factKindContentEntity,
		Payload: map[string]any{
			"entity_id":       entityID,
			"entity_type":     entityType,
			"entity_metadata": metadata,
		},
	}
}

// TestExtractCrossplaneSatisfiedByEdgeRowsResolvesRealisticClaim is the
// failing-first proof for issue #5347: a realistic Claim (custom-group
// apiVersion, no ".crossplane.io/" substring) must resolve to exactly one
// SATISFIED_BY edge against the XRD whose (spec.group, spec.claimNames.kind)
// matches. Before the #5347 fix this candidate never reached the reducer at
// all (the parser bucketed it as crossplane_claims, a dead sink no writer
// consumed) — this proves the live derivation end to end.
func TestExtractCrossplaneSatisfiedByEdgeRowsResolvesRealisticClaim(t *testing.T) {
	t.Parallel()

	claim := crossplaneContentEntityEnvelope("k8s:claim-1", crossplaneEntityTypeK8sResource, map[string]any{
		"api_version": "database.example.org/v1alpha1",
		"kind":        "PostgreSQLInstance",
	})
	xrd := crossplaneContentEntityEnvelope("xrd:1", crossplaneEntityTypeXRD, map[string]any{
		"group":      "database.example.org",
		"kind":       "XPostgreSQLInstance",
		"claim_kind": "PostgreSQLInstance",
	})

	rows, tally, err := ExtractCrossplaneSatisfiedByEdgeRows([]facts.Envelope{claim, xrd})
	if err != nil {
		t.Fatalf("ExtractCrossplaneSatisfiedByEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %#v", len(rows), rows)
	}
	if got := rows[0]["claim_uid"]; got != "k8s:claim-1" {
		t.Errorf("rows[0][claim_uid] = %v, want k8s:claim-1", got)
	}
	if got := rows[0]["xrd_uid"]; got != "xrd:1" {
		t.Errorf("rows[0][xrd_uid] = %v, want xrd:1", got)
	}
	if got := rows[0]["rel_type"]; got != "SATISFIED_BY" {
		t.Errorf("rows[0][rel_type] = %v, want SATISFIED_BY", got)
	}
	if got := tally.totalMaterialized(); got != 1 {
		t.Errorf("tally.totalMaterialized() = %d, want 1", got)
	}
	if tally.ambiguousSkipped != 0 {
		t.Errorf("tally.ambiguousSkipped = %d, want 0", tally.ambiguousSkipped)
	}
}

// TestExtractCrossplaneSatisfiedByEdgeRowsNegativeCases proves the domain
// never fabricates a SATISFIED_BY edge for evidence that is not a real,
// unambiguously-resolvable Claim: a provider Managed Resource (the row the
// old inverted parser classifier mistakenly bucketed as a claim), a
// Composite Resource (XR) whose kind matches the XRD's names.kind rather than
// claimNames.kind, and an ordinary unrelated Kubernetes object.
func TestExtractCrossplaneSatisfiedByEdgeRowsNegativeCases(t *testing.T) {
	t.Parallel()

	xrd := crossplaneContentEntityEnvelope("xrd:1", crossplaneEntityTypeXRD, map[string]any{
		"group":      "database.example.org",
		"kind":       "XPostgreSQLInstance",
		"claim_kind": "PostgreSQLInstance",
	})

	providerManagedResource := crossplaneContentEntityEnvelope("k8s:mr-1", crossplaneEntityTypeK8sResource, map[string]any{
		"api_version": "ec2.aws.crossplane.io/v1alpha1",
		"kind":        "Instance",
	})
	// The XR (Composite Resource) uses names.kind ("XPostgreSQLInstance"),
	// not claimNames.kind ("PostgreSQLInstance"). Crossplane requires these
	// to be distinct, so keying the XRD index on claimNames alone excludes
	// XRs by construction.
	compositeResource := crossplaneContentEntityEnvelope("k8s:xr-1", crossplaneEntityTypeK8sResource, map[string]any{
		"api_version": "database.example.org/v1alpha1",
		"kind":        "XPostgreSQLInstance",
	})
	unrelated := crossplaneContentEntityEnvelope("k8s:deploy-1", crossplaneEntityTypeK8sResource, map[string]any{
		"api_version": "apps/v1",
		"kind":        "Deployment",
	})

	rows, tally, err := ExtractCrossplaneSatisfiedByEdgeRows([]facts.Envelope{
		xrd, providerManagedResource, compositeResource, unrelated,
	})
	if err != nil {
		t.Fatalf("ExtractCrossplaneSatisfiedByEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 (no candidate resolves): %#v", len(rows), rows)
	}
	if got := tally.totalMaterialized(); got != 0 {
		t.Errorf("tally.totalMaterialized() = %d, want 0", got)
	}
	if tally.ambiguousSkipped != 0 {
		t.Errorf("tally.ambiguousSkipped = %d, want 0 (zero-match is not ambiguous)", tally.ambiguousSkipped)
	}
}

// TestExtractCrossplaneSatisfiedByEdgeRowsAmbiguousSkipsNoFabrication proves
// that when a Claim candidate's (group, kind) matches two or more XRD nodes,
// the domain writes no edge and never picks a fabricated representative — it
// only counts the ambiguity.
func TestExtractCrossplaneSatisfiedByEdgeRowsAmbiguousSkipsNoFabrication(t *testing.T) {
	t.Parallel()

	claim := crossplaneContentEntityEnvelope("k8s:claim-1", crossplaneEntityTypeK8sResource, map[string]any{
		"api_version": "database.example.org/v1alpha1",
		"kind":        "PostgreSQLInstance",
	})
	xrdA := crossplaneContentEntityEnvelope("xrd:a", crossplaneEntityTypeXRD, map[string]any{
		"group":      "database.example.org",
		"claim_kind": "PostgreSQLInstance",
	})
	xrdB := crossplaneContentEntityEnvelope("xrd:b", crossplaneEntityTypeXRD, map[string]any{
		"group":      "database.example.org",
		"claim_kind": "PostgreSQLInstance",
	})

	rows, tally, err := ExtractCrossplaneSatisfiedByEdgeRows([]facts.Envelope{claim, xrdA, xrdB})
	if err != nil {
		t.Fatalf("ExtractCrossplaneSatisfiedByEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 (ambiguous match must not fabricate an edge): %#v", len(rows), rows)
	}
	if tally.ambiguousSkipped != 1 {
		t.Errorf("tally.ambiguousSkipped = %d, want 1", tally.ambiguousSkipped)
	}
	if got := tally.totalMaterialized(); got != 0 {
		t.Errorf("tally.totalMaterialized() = %d, want 0", got)
	}
}

// TestExtractCrossplaneSatisfiedByEdgeRowsUnresolvedXRDProducesNoEdge proves
// an XRD with no matching Claim in this generation's candidate set produces
// no edge and no tally noise — the same "absence is not an error" contract
// as the zero-match Claim case.
func TestExtractCrossplaneSatisfiedByEdgeRowsUnresolvedXRDProducesNoEdge(t *testing.T) {
	t.Parallel()

	xrd := crossplaneContentEntityEnvelope("xrd:1", crossplaneEntityTypeXRD, map[string]any{
		"group":      "database.example.org",
		"claim_kind": "PostgreSQLInstance",
	})

	rows, tally, err := ExtractCrossplaneSatisfiedByEdgeRows([]facts.Envelope{xrd})
	if err != nil {
		t.Fatalf("ExtractCrossplaneSatisfiedByEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
	if got := tally.totalMaterialized(); got != 0 {
		t.Errorf("tally.totalMaterialized() = %d, want 0", got)
	}
	if tally.ambiguousSkipped != 0 {
		t.Errorf("tally.ambiguousSkipped = %d, want 0", tally.ambiguousSkipped)
	}
}

// TestExtractCrossplaneSatisfiedByEdgeRowsExcludesEmptyGroupOrClaimKind
// proves core-group Claims (apiVersion with no "/") and cluster-scoped/
// malformed XRDs (empty claim_kind) never match on the empty string.
func TestExtractCrossplaneSatisfiedByEdgeRowsExcludesEmptyGroupOrClaimKind(t *testing.T) {
	t.Parallel()

	coreGroupClaim := crossplaneContentEntityEnvelope("k8s:core-1", crossplaneEntityTypeK8sResource, map[string]any{
		"api_version": "v1",
		"kind":        "ConfigMap",
	})
	xrdNoClaimKind := crossplaneContentEntityEnvelope("xrd:no-claim", crossplaneEntityTypeXRD, map[string]any{
		"group": "",
	})

	rows, tally, err := ExtractCrossplaneSatisfiedByEdgeRows([]facts.Envelope{coreGroupClaim, xrdNoClaimKind})
	if err != nil {
		t.Fatalf("ExtractCrossplaneSatisfiedByEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 (empty group/claim_kind must never match)", len(rows))
	}
	if got := tally.totalMaterialized() + tally.ambiguousSkipped; got != 0 {
		t.Errorf("tally has %d entries, want 0", got)
	}
}
