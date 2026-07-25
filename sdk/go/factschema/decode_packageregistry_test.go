// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	packageregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/packageregistry/v1"
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

// TestDecodePackageRegistryPackageArtifact_ColonBearingHashAlgorithmDeadLetters
// is the shaped regression for the #5458 P2 review finding: hashes is stored
// on the graph as a sorted "algorithm:digest" string list because Cypher node
// properties cannot hold a nested map (packageRegistryHashPairs in
// go/internal/storage/cypher/package_registry_artifact_writer.go), and that
// split is only unambiguous when the algorithm name itself never contains
// ':'. Before this validation, a collector-supplied algorithm name containing
// ':' would silently produce an ambiguous "algorithm:digest" encoding on the
// graph node with no error anywhere in the pipeline. This proves the payload
// dead-letters as a classified input_invalid naming "hashes" instead.
func TestDecodePackageRegistryPackageArtifact_ColonBearingHashAlgorithmDeadLetters(t *testing.T) {
	t.Parallel()

	payload := fullPackageArtifactPayload()
	payload["hashes"] = map[string]any{
		"sha256:extra": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}

	env := Envelope{FactKind: FactKindPackageRegistryPackageArtifact, SchemaVersion: "1.0.0", Payload: payload}
	got, err := DecodePackageRegistryPackageArtifact(env)
	if err == nil {
		t.Fatalf("DecodePackageRegistryPackageArtifact() error = nil, want non-nil for colon-bearing hash algorithm")
	}

	var classified *DecodeError
	if !errors.As(err, &classified) {
		t.Fatalf("DecodePackageRegistryPackageArtifact() error = %T, want *DecodeError", err)
	}
	if classified.Classification != ClassificationInputInvalid {
		t.Fatalf("Classification = %q, want %q", classified.Classification, ClassificationInputInvalid)
	}
	// This is an invalid VALUE on a present field, not a missing required
	// key, so Field stays unset and the offending field name is embedded in
	// the error message instead — the same shape decodePayloadFieldError
	// uses for the malformed-from_port case in decode_aws.go. Assert on the
	// message rather than DecodeError.Field.
	if !strings.Contains(classified.Error(), "hashes") {
		t.Fatalf("error message = %q, want it to name the %q field", classified.Error(), "hashes")
	}

	var zero packageregistryv1.PackageArtifact
	if !reflect.DeepEqual(got, zero) {
		t.Fatalf("DecodePackageRegistryPackageArtifact() returned non-zero struct %+v on error, want zero value", got)
	}
}

// TestDecodePackageRegistryPackageArtifact_FullPayloadDecodes is the positive
// counterpart: a payload carrying every required key and colon-free hash
// algorithm names decodes cleanly, so the rejection above cannot pass merely
// because every payload fails.
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
