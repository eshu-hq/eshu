package facts

import "testing"

func TestAWSFactKindRegistry(t *testing.T) {
	kinds := AWSFactKinds()
	want := []string{
		AWSResourceFactKind,
		AWSRelationshipFactKind,
		AWSTagObservationFactKind,
		AWSDNSRecordFactKind,
		AWSImageReferenceFactKind,
		AWSWarningFactKind,
	}
	if len(kinds) != len(want) {
		t.Fatalf("len(AWSFactKinds()) = %d, want %d", len(kinds), len(want))
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("AWSFactKinds()[%d] = %q, want %q", i, kinds[i], want[i])
		}
		version, ok := AWSSchemaVersion(kinds[i])
		if !ok {
			t.Fatalf("AWSSchemaVersion(%q) ok = false", kinds[i])
		}
		if version != "1.0.0" {
			t.Fatalf("AWSSchemaVersion(%q) = %q, want 1.0.0", kinds[i], version)
		}
	}

	kinds[0] = "mutated"
	if got := AWSFactKinds()[0]; got != AWSResourceFactKind {
		t.Fatalf("AWSFactKinds returned mutable backing slice, got first kind %q", got)
	}
}
