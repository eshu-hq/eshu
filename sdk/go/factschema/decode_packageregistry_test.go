// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"testing"
)

// fullPackageArtifactPayload returns a valid package_registry.package_artifact
// payload with every required key present, so a test can mutate exactly one
// field and prove decode dead-letters on it.
func fullPackageArtifactPayload() map[string]any {
	return map[string]any{
		"package_id":   "package://npm/registry.npmjs.org/left-pad",
		"version_id":   "package-version://npm/registry.npmjs.org/left-pad@1.3.0",
		"artifact_key": "left-pad-1.3.0.tgz",
		"hashes": map[string]any{
			"sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
	}
}

// TestDecodePackageRegistryPackageArtifact_ColonBearingHashAlgorithmDecodesCleanly
// is the shaped regression for the #5820 P2 review finding: an earlier version
// of this decode rejected a colon-bearing hashes algorithm name as
// input_invalid, reasoning that the graph writer flattens Hashes into a sorted
// "algorithm:digest" string list (packageRegistryHashPairs in
// go/internal/storage/cypher/package_registry_artifact_writer.go) and a colon
// inside the algorithm name would make that split ambiguous. But
// package_registry.package_artifact.v1.schema.json's hashes.additionalProperties
// accepts ANY string key, and this same schema version previously decoded
// these payloads — so the rejection silently narrowed the public v1 contract
// without a major bump or compatibility shim. The fix moves unambiguity from
// rejection to a lossless escaping scheme in packageRegistryHashPairs itself
// (see TestPackageRegistryHashPairsRoundTripsColonBearingAlgorithm in
// go/internal/storage/cypher), so decode no longer needs to reject anything: a
// colon-bearing algorithm name is exactly as valid as any other string key and
// must decode cleanly, keeping every value the v1 schema allows decodable.
func TestDecodePackageRegistryPackageArtifact_ColonBearingHashAlgorithmDecodesCleanly(t *testing.T) {
	t.Parallel()

	payload := fullPackageArtifactPayload()
	payload["hashes"] = map[string]any{
		"sha256:extra": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}

	env := Envelope{FactKind: FactKindPackageRegistryPackageArtifact, SchemaVersion: "1.0.0", Payload: payload}
	got, err := DecodePackageRegistryPackageArtifact(env)
	if err != nil {
		t.Fatalf("DecodePackageRegistryPackageArtifact() error = %v, want nil for a colon-bearing hash algorithm (v1 schema allows any string key)", err)
	}
	if got, want := got.Hashes["sha256:extra"], "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"; got != want {
		t.Fatalf("Hashes[%q] = %q, want %q", "sha256:extra", got, want)
	}
}

// TestDecodePackageRegistryPackageArtifact_FullPayloadDecodes is the baseline
// positive case: a payload carrying every required key and a plain hash
// algorithm name decodes cleanly.
func TestDecodePackageRegistryPackageArtifact_FullPayloadDecodes(t *testing.T) {
	t.Parallel()

	env := Envelope{
		FactKind:      FactKindPackageRegistryPackageArtifact,
		SchemaVersion: "1.0.0",
		Payload:       fullPackageArtifactPayload(),
	}
	artifact, err := DecodePackageRegistryPackageArtifact(env)
	if err != nil {
		t.Fatalf("DecodePackageRegistryPackageArtifact() error = %v, want nil", err)
	}
	if got, want := artifact.Hashes["sha256"], "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"; got != want {
		t.Fatalf("Hashes[sha256] = %q, want %q", got, want)
	}
}
