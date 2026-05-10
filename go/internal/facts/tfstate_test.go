package facts

import (
	"slices"
	"testing"
)

func TestTerraformStateFactKindsAreCompleteAndOrdered(t *testing.T) {
	t.Parallel()

	want := []string{
		TerraformStateCandidateFactKind,
		TerraformStateSnapshotFactKind,
		TerraformStateResourceFactKind,
		TerraformStateOutputFactKind,
		TerraformStateModuleFactKind,
		TerraformStateProviderBindingFactKind,
		TerraformStateTagObservationFactKind,
		TerraformStateWarningFactKind,
	}

	got := TerraformStateFactKinds()

	if !slices.Equal(got, want) {
		t.Fatalf("TerraformStateFactKinds() = %#v, want %#v", got, want)
	}
	for _, kind := range got {
		if kind == "" {
			t.Fatalf("TerraformStateFactKinds() contains blank kind: %#v", got)
		}
		if version, ok := TerraformStateSchemaVersion(kind); !ok || version != "1.0.0" {
			t.Fatalf("TerraformStateSchemaVersion(%q) = %q, %v, want 1.0.0, true", kind, version, ok)
		}
	}
}

func TestTerraformStateFactKindsReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()

	kinds := TerraformStateFactKinds()
	firstKind := kinds[0]
	kinds[0] = "mutated"

	if got := TerraformStateFactKinds()[0]; got != firstKind {
		t.Fatalf("TerraformStateFactKinds()[0] = %q, want %q", got, firstKind)
	}
}

func TestTerraformStateSchemaVersionRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	if version, ok := TerraformStateSchemaVersion("terraform_state_unknown"); ok || version != "" {
		t.Fatalf("TerraformStateSchemaVersion(unknown) = %q, %v, want empty, false", version, ok)
	}
}
