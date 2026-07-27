// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// The B-7 corpus shape these tests reproduce: one digest that a CI run reports
// building AND a deploying repository's content_entity names by explicit image
// reference. Both evidence paths resolve to the same image reference, the same
// exact_digest outcome, and therefore the same durable stable fact key.
const (
	ciDigestProvenanceDigest    = "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab"
	ciDigestProvenanceBuildRepo = "repository:r_69256c06"
	ciDigestProvenanceRunRepo   = "repository:r_217415d9"
	ciDigestProvenanceRegistry  = "ghcr.io"
	ciDigestProvenanceImageRepo = "eshu-hq/supply-chain-demo"
	ciDigestProvenanceImageRef  = ciDigestProvenanceRegistry + "/" + ciDigestProvenanceImageRepo +
		"@" + ciDigestProvenanceDigest
	ciDigestProvenanceCommitSHA = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c"
)

// ciDigestProvenanceEnvelopes is the corpus-shaped evidence set: the deploying
// repository's manifest reference, the building run and its digest-only
// artifact, and the registry observation that resolves both to one image.
func ciDigestProvenanceEnvelopes() []facts.Envelope {
	return []facts.Envelope{
		{
			FactID:   "content-entity-deploying-manifest",
			FactKind: factKindContentEntity,
			Payload: map[string]any{
				"repository_id": ciDigestProvenanceRunRepo,
				"entity_metadata": map[string]any{
					"container_images": []any{ciDigestProvenanceImageRef},
				},
			},
		},
		ciRunFact("5150", "github_actions", ciDigestProvenanceBuildRepo, ciDigestProvenanceCommitSHA),
		ciArtifactFact("ci-artifact-5150", "5150", ciDigestProvenanceDigest),
		ociManifestFactForRepository(
			"oci-manifest-supply-chain-demo",
			ciDigestProvenanceRegistry,
			ciDigestProvenanceImageRepo,
			ciDigestProvenanceDigest,
		),
	}
}

// TestCIRunDigestAnchorConfersBuildProvenanceOnCompetingImageRefDecision is the
// failing-then-green proof for the gap #5808 left behind.
//
// #5808 taught addContainerImageDigestRef to keep buildProvenanceRepositoryIDs,
// which fixes the decision raised by the bare-digest ref a ci.artifact creates.
// It does NOT fix the decision that actually gets persisted when a deploying
// repository names the same digest by explicit image reference: both refs
// classify to the same (image_ref, outcome) pair, so they share one durable
// stable fact key (containerImageIdentityStableFactKey) and one wins the upsert.
// The winner is the explicit-reference decision, which reaches the CI run only
// through applyCIRunDigestRevision -- and that function copied the run's
// repository into SourceRepositoryIDs while leaving BuildProvenanceRepositoryIDs
// untouched, unlike its SLSA sibling applySLSADigestRevision.
//
// The result was a persisted identity row naming the building repository in
// source_repository_ids and nothing at all in build_provenance_repository_ids.
// Every consumer that joins on build provenance -- cicdImageMatchesForRepository
// most of all -- then had nothing to match, so a CI run that demonstrably built
// the digest could not narrow its own image identity and its correlation
// degraded to ambiguous/provenance-only. Measured on the live B-7 corpus before
// this fix, for exactly this digest:
//
//	scope ci_cd_run:github_actions:eshu-hq:supply-chain-demo
//	  source_repository_ids           = ["repository:r_217415d9", "repository:r_69256c06"]
//	  build_provenance_repository_ids = null
//
// Asserting across EVERY decision that resolves the digest (rather than only the
// bare-digest one #5808 already covers) is deliberate: whichever decision wins
// the shared stable fact key is the row downstream reducers read, so the
// provenance has to survive on all of them.
func TestCIRunDigestAnchorConfersBuildProvenanceOnCompetingImageRefDecision(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions(ciDigestProvenanceEnvelopes())

	resolved := 0
	for _, decision := range decisions {
		if decision.Digest != ciDigestProvenanceDigest {
			continue
		}
		resolved++
		if !slices.Contains(decision.BuildProvenanceRepositoryIDs, ciDigestProvenanceBuildRepo) {
			t.Fatalf(
				"decision %q (%s) must carry the building repository in BuildProvenanceRepositoryIDs: got %#v, want %q",
				decision.ImageRef, decision.Reason, decision.BuildProvenanceRepositoryIDs, ciDigestProvenanceBuildRepo,
			)
		}
	}
	if resolved == 0 {
		t.Fatalf("no decision resolved digest %q; the fixture no longer reproduces the corpus shape", ciDigestProvenanceDigest)
	}
}

// TestCIRunDigestBuildProvenanceLetsCorrelationEscapeProvenanceOnly proves the
// consequence the fix exists for, one hop downstream, through the PERSISTED
// payload rather than the in-memory decision.
//
// containerImageIdentityPayload is the exact map the writer marshals, so this
// exercises the same payload key cicdRunCorrelationImageIdentity decodes. The
// bare-digest decision is deliberately excluded from the identity envelopes: it
// loses the shared stable fact key in production, so admitting it here would
// hide the very regression this test guards.
//
// The deploy-only sibling rows are equally deliberate. The digest carries 16
// identity rows in the live corpus -- one per scope that observed it, and every
// one of them a deploying/referencing scope except the CI scope's own row -- so
// cicdImageMatchesForRepository is what stands between an ambiguous multi-row
// match and an exact one. A single-row fixture would classify exact even with
// the provenance dropped, which is precisely the false green that let this gap
// survive.
//
// Before the fix the correlation lands ambiguous + provenance_only, which
// matchingSupplyChainDeployments (supply_chain_impact_runtime.go) rejects
// outright -- so the deployment's environment and its #5425 environment_evidence
// never reach a supply-chain impact finding.
func TestCIRunDigestBuildProvenanceLetsCorrelationEscapeProvenanceOnly(t *testing.T) {
	t.Parallel()

	write := ContainerImageIdentityWrite{
		IntentID:     "intent-1",
		ScopeID:      "ci_cd_run:github_actions:eshu-hq:supply-chain-demo",
		GenerationID: "gen-1",
		SourceSystem: "ci_cd_run",
	}

	envelopes := []facts.Envelope{
		ciRunFact("5150", "github_actions", ciDigestProvenanceBuildRepo, ciDigestProvenanceCommitSHA),
		ciArtifactFact("ci-artifact-5150", "5150", ciDigestProvenanceDigest),
	}
	// Emulate the writer's upsert rather than admitting every decision: the two
	// decisions for this digest share one fact ID, and reducerBatchInsertFacts
	// resolves that collision last-write-wins. Admitting both would smuggle the
	// bare-digest decision's provenance into the index, and the correlation
	// would narrow on a row Postgres never kept.
	persisted := map[string]facts.Envelope{}
	order := make([]string, 0, 2)
	for _, decision := range BuildContainerImageIdentityDecisions(ciDigestProvenanceEnvelopes()) {
		if decision.Digest != ciDigestProvenanceDigest || decision.ImageRef != ciDigestProvenanceImageRef {
			continue
		}
		factID := containerImageIdentityFactID(write, decision)
		if _, seen := persisted[factID]; !seen {
			order = append(order, factID)
		}
		persisted[factID] = facts.Envelope{
			FactID:   factID,
			FactKind: containerImageIdentityFactKind,
			Payload: containerImageIdentityPayload(
				write, decision, canonicalContainerImageIdentityID(write, decision),
			),
		}
	}
	if len(order) != 1 {
		t.Fatalf(
			"want exactly 1 persisted identity fact for %q (the two decisions share a stable fact key), got %d: %v",
			ciDigestProvenanceImageRef, len(order), order,
		)
	}
	envelopes = append(envelopes, persisted[order[0]])
	envelopes = append(envelopes, ciDigestProvenanceDeployOnlyIdentityFacts(t)...)

	decisions := BuildCICDRunCorrelationDecisions(envelopes)
	if len(decisions) != 1 {
		t.Fatalf("decisions = %d, want 1: %#v", len(decisions), decisions)
	}
	decision := decisions[0]
	if decision.Outcome != CICDRunCorrelationExact {
		t.Fatalf("Outcome = %q (%s), want %q", decision.Outcome, decision.Reason, CICDRunCorrelationExact)
	}
	if decision.ProvenanceOnly {
		t.Fatalf("ProvenanceOnly = true (%s); matchingSupplyChainDeployments rejects a provenance-only deployment", decision.Reason)
	}
}

// ciDigestProvenanceDeployOnlyIdentityFacts builds the sibling identity rows a
// deploying scope publishes for the same digest: a content_entity naming the
// image by explicit reference, with no CI evidence at all, so the decision
// carries the deploying repository in source_repository_ids and nothing in
// build_provenance_repository_ids. Two are enough to make the unfiltered digest
// match multi-row, which is what forces cicdImageMatchesForRepository to be the
// deciding join rather than an unused pass-through.
func ciDigestProvenanceDeployOnlyIdentityFacts(t *testing.T) []facts.Envelope {
	t.Helper()

	out := make([]facts.Envelope, 0, 2)
	for _, scopeID := range []string{
		"git-repository-scope:" + ciDigestProvenanceRunRepo,
		"aws:123456789012:us-east-1:ecs",
	} {
		write := ContainerImageIdentityWrite{
			IntentID:     "intent-" + scopeID,
			ScopeID:      scopeID,
			GenerationID: "gen-1",
			SourceSystem: "deploy",
		}
		found := false
		for _, decision := range BuildContainerImageIdentityDecisions([]facts.Envelope{
			{
				FactID:   "content-entity-" + scopeID,
				FactKind: factKindContentEntity,
				Payload: map[string]any{
					"repository_id": ciDigestProvenanceRunRepo,
					"entity_metadata": map[string]any{
						"container_images": []any{ciDigestProvenanceImageRef},
					},
				},
			},
			ociManifestFactForRepository(
				"oci-manifest-"+scopeID,
				ciDigestProvenanceRegistry,
				ciDigestProvenanceImageRepo,
				ciDigestProvenanceDigest,
			),
		}) {
			if decision.ImageRef != ciDigestProvenanceImageRef {
				continue
			}
			if len(decision.BuildProvenanceRepositoryIDs) != 0 {
				t.Fatalf(
					"deploy-only sibling for %q must carry no build provenance, got %#v",
					scopeID, decision.BuildProvenanceRepositoryIDs,
				)
			}
			found = true
			out = append(out, facts.Envelope{
				FactID:   "reducer_container_image_identity:" + scopeID,
				FactKind: containerImageIdentityFactKind,
				Payload: containerImageIdentityPayload(
					write, decision, canonicalContainerImageIdentityID(write, decision),
				),
			})
		}
		if !found {
			t.Fatalf("no deploy-only decision resolved image ref %q for scope %q", ciDigestProvenanceImageRef, scopeID)
		}
	}
	return out
}
