package facts

import "testing"

func TestSBOMAttestationFactKindsAndSchemaVersions(t *testing.T) {
	t.Parallel()

	want := []string{
		SBOMDocumentFactKind,
		SBOMComponentFactKind,
		SBOMDependencyRelationshipFactKind,
		SBOMExternalReferenceFactKind,
		AttestationStatementFactKind,
		AttestationSLSAProvenanceFactKind,
		AttestationSignatureVerificationFactKind,
		SBOMWarningFactKind,
	}
	got := SBOMAttestationFactKinds()
	if len(got) != len(want) {
		t.Fatalf("SBOMAttestationFactKinds() len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i, kind := range want {
		if got[i] != kind {
			t.Fatalf("SBOMAttestationFactKinds()[%d] = %q, want %q", i, got[i], kind)
		}
		version, ok := SBOMAttestationSchemaVersion(kind)
		if !ok || version != SBOMAttestationSchemaVersionV1 {
			t.Fatalf("SBOMAttestationSchemaVersion(%q) = %q, %v, want %q, true", kind, version, ok, SBOMAttestationSchemaVersionV1)
		}
	}
}

func TestSBOMAttestationFactKindsReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()

	kinds := SBOMAttestationFactKinds()
	kinds[0] = "mutated"
	if got := SBOMAttestationFactKinds()[0]; got != SBOMDocumentFactKind {
		t.Fatalf("SBOMAttestationFactKinds()[0] = %q, want %q", got, SBOMDocumentFactKind)
	}
}
