// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestFilterContainerImageCIFactsForOwnerRunKeyIncludesFactScope is the
// #5810 P1 follow-up regression: filterContainerImageCIFactsForOwner
// (container_image_identity_ci_loader.go) joined ci.run to ci.artifact by
// provider/run_id/run_attempt alone (cicdRunKeyFromParts). Two INDEPENDENT
// CI installations -- github.com and a self-hosted GHES instance, or two
// separate self-hosted runners -- mint their own run_id counters
// independently, so the same (provider, run_id, run_attempt) tuple can
// legitimately name two UNRELATED runs living in two different scopes. The
// owner-gate's first pass correctly restricts ownedRunKeys to runs whose OWN
// repository_id equals the owner, but the second pass re-derives the SAME
// scope-oblivious key for every candidate envelope and admits any match --
// so a foreign scope's run and artifact, sharing only the tuple, both slip
// into the owner's kept set. Downstream, containerImageCIRuns
// (container_image_identity_typed_evidence.go) indexes runs by that same bare
// tuple key in a plain map, so the foreign run can even overwrite the
// owner's real run anchor, letting a completely unrelated image built by a
// different CI installation attach to the owner's repository.
//
// The fix must fold the fact's own scope_id into the join key so a tuple
// collision across two scopes can never join.
func TestFilterContainerImageCIFactsForOwnerRunKeyIncludesFactScope(t *testing.T) {
	t.Parallel()

	const (
		ownerRepoID   = "repository:r_owner_5810scopekey"
		provider      = "github_actions"
		sharedRunID   = "777"
		digestOwned   = "sha256:5810k1k1k1k1k1k1k1k1k1k1k1k1k1k1k1k1k1k1k1k1k1k1k1k1k1k1k1k1k1k1"
		digestForeign = "sha256:5810k2k2k2k2k2k2k2k2k2k2k2k2k2k2k2k2k2k2k2k2k2k2k2k2k2k2k2k2k2k2"
	)

	ownedRun := ciRunFact(sharedRunID, provider, ownerRepoID, "commit-owned")
	ownedRun.ScopeID = "ci-run-scope:github.com"

	ownedArtifact := ciArtifactFact("artifact-owned-5810scopekey", sharedRunID, digestOwned)
	ownedArtifact.ScopeID = "ci-run-scope:github.com"

	// A DIFFERENT CI installation (self-hosted GHES) whose own run_id
	// counter independently reached the same number, evidencing a
	// completely unrelated digest. Its repository_id is deliberately empty
	// (a run with no repository anchor at all) -- the vulnerability under
	// test is the second-pass key JOIN, not the first-pass owner check,
	// which already correctly excludes this run from ownedRunKeys.
	foreignRun := ciRunFact(sharedRunID, provider, "", "commit-foreign")
	foreignRun.FactID = "ci.run:foreign-5810scopekey"
	foreignRun.ScopeID = "ci-run-scope:ghes.example.com"

	foreignArtifact := ciArtifactFact("artifact-foreign-5810scopekey", sharedRunID, digestForeign)
	foreignArtifact.ScopeID = "ci-run-scope:ghes.example.com"

	kept := filterContainerImageCIFactsForOwner(
		[]facts.Envelope{ownedRun, ownedArtifact, foreignRun, foreignArtifact},
		ownerRepoID,
	)

	keptDigests := map[string]bool{}
	keptFactIDs := map[string]bool{}
	for _, envelope := range kept {
		keptFactIDs[envelope.FactID] = true
		if envelope.FactKind != facts.CICDArtifactFactKind {
			continue
		}
		artifact, err := decodeCICDArtifact(envelope)
		if err != nil {
			t.Fatalf("decodeCICDArtifact(%q) error = %v", envelope.FactID, err)
		}
		keptDigests[trimmedCICDPtr(artifact.ArtifactDigest)] = true
	}
	if !keptDigests[digestOwned] {
		t.Fatalf("kept digests = %#v, want the owner's own artifact %q admitted", keptDigests, digestOwned)
	}
	if keptDigests[digestForeign] {
		t.Fatalf(
			"kept digests = %#v: a DIFFERENT CI installation's run sharing the "+
				"same provider/run_id/run_attempt tuple must not join its "+
				"artifact into the owner's set -- the join key must include the "+
				"fact's own scope, not just the tuple",
			keptDigests,
		)
	}
	if keptFactIDs[foreignRun.FactID] {
		t.Fatalf(
			"kept envelopes = %#v: the foreign run must not be admitted -- if it "+
				"slipped through, containerImageCIRuns' key-indexed map could let "+
				"it win the shared key and blank or overwrite the owner's own run "+
				"anchor",
			kept,
		)
	}
}
