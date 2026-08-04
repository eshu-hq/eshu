// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestCICDNarrowingSelectsTheBuilderFromPublishedFacts is the base-portable
// regression for #5766 and #5823 together. It drives real published identity
// payloads through buildCICDImageIdentityIndex and then narrows, so it compiles
// and runs unchanged against origin/main as well as this branch -- a compile
// failure is not a red-then-green proof, and the struct-literal tests in
// ci_cd_run_correlation_repo_narrowing_test.go cannot run against a base that
// lacks their fields.
//
// On origin/main this FAILS at the first assertion: narrowing compared the
// identity payload's own repository_id -- an OCI registry path -- against the
// run's canonical repository:r_... id, matched nothing, and left the caller
// with an unfiltered two-row set that degrades to ambiguous.
//
// The second assertion is what distinguishes #5823 from a naive #5766 fix:
// narrowing on source_repository_ids alone would select the deploy-only row
// here and promote a repository that never built the image to exact.
func TestCICDNarrowingSelectsTheBuilderFromPublishedFacts(t *testing.T) {
	t.Parallel()

	const (
		digest        = "sha256:9999999999999999999999999999999999999999999999999999999999999999"
		ociPath       = "oci-registry://ghcr.io/eshu-hq/demo"
		builderRepo   = "repository:r_builder"
		deployingRepo = "repository:r_deployer"
	)

	index := buildCICDImageIdentityIndex([]facts.Envelope{
		{
			FactID:   "identity-builder",
			FactKind: containerImageIdentityFactKind,
			Payload: map[string]any{
				"digest":                          digest,
				"repository_id":                   ociPath,
				"image_ref":                       "ghcr.io/eshu-hq/demo:v1",
				"source_repository_ids":           []any{builderRepo},
				"build_provenance_repository_ids": []any{builderRepo},
			},
		},
		{
			FactID:   "identity-deployer-reference",
			FactKind: containerImageIdentityFactKind,
			Payload: map[string]any{
				"digest":        digest,
				"repository_id": ociPath,
				"image_ref":     "ghcr.io/eshu-hq/demo:v1",
				// The deploying repository's manifest references this digest.
				// It did not build it, so it earns no build provenance.
				"source_repository_ids":           []any{deployingRepo},
				"build_provenance_repository_ids": []any{},
			},
		},
	})

	matches := index[digest]
	if len(matches) != 1 {
		t.Fatalf("index[%q] = %d identities, want one image identity backed by two support facts", digest, len(matches))
	}
	if got := matches[0].evidenceFactIDs; !slices.Equal(
		got,
		[]string{"identity-builder", "identity-deployer-reference"},
	) {
		t.Fatalf("identity evidence = %#v, want both support facts", got)
	}

	// #5766: the builder must narrow to the canonical identity because one of
	// its support rows carries this repository's build provenance.
	builderMatches := cicdImageMatchesForRepository(matches, builderRepo)
	if len(builderMatches) != 1 || builderMatches[0].factID != "identity-builder" {
		t.Fatalf("narrowing for the builder = %d rows (%#v), want exactly the identity-builder row; "+
			"comparing the identity's OCI repository_id against a canonical repository:r_... id never matches (#5766)",
			len(builderMatches), builderMatches)
	}

	// #5823: a repository that only references the digest must narrow to
	// nothing, so the caller keeps the ambiguous two-row set instead of
	// promoting a non-builder to exact.
	if got := cicdImageMatchesForRepository(matches, deployingRepo); len(got) != 0 {
		t.Fatalf("narrowing for the deploying repository = %#v, want 0 rows: "+
			"source_repository_ids conflates built-from with referenced-by (#5823)", got)
	}
}
