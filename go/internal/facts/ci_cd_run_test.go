package facts

import "testing"

func TestCICDRunFactKindsAndSchemaVersions(t *testing.T) {
	t.Parallel()

	kinds := CICDRunFactKinds()
	want := []string{
		CICDPipelineDefinitionFactKind,
		CICDRunFactKind,
		CICDJobFactKind,
		CICDStepFactKind,
		CICDArtifactFactKind,
		CICDTriggerEdgeFactKind,
		CICDEnvironmentObservationFactKind,
		CICDWarningFactKind,
	}
	if len(kinds) != len(want) {
		t.Fatalf("CICDRunFactKinds() len = %d, want %d", len(kinds), len(want))
	}
	for i, kind := range want {
		if got := kinds[i]; got != kind {
			t.Fatalf("CICDRunFactKinds()[%d] = %q, want %q", i, got, kind)
		}
		version, ok := CICDRunSchemaVersion(kind)
		if !ok {
			t.Fatalf("CICDRunSchemaVersion(%q) ok = false, want true", kind)
		}
		if version != CICDSchemaVersion {
			t.Fatalf("CICDRunSchemaVersion(%q) = %q, want %q", kind, version, CICDSchemaVersion)
		}
	}

	kinds[0] = "mutated"
	if got := CICDRunFactKinds()[0]; got != CICDPipelineDefinitionFactKind {
		t.Fatalf("CICDRunFactKinds() returned mutable backing slice, got %q", got)
	}
}
