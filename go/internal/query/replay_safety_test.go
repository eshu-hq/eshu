package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/semanticqueue"
)

// TestUnsafeReplayClassesPinnedToSourceConstants keeps the refusal set bound to
// the authoritative failure-class contracts so it never silently drifts.
func TestUnsafeReplayClassesPinnedToSourceConstants(t *testing.T) {
	if _, ok := unsafeReplayRefusal(string(projector.FailureClassInputInvalid)); !ok {
		t.Fatalf("projector input_invalid (non-retryable) must be an unsafe replay class")
	}
	if _, ok := unsafeReplayRefusal(string(semanticqueue.StatusUnsafePayload)); !ok {
		t.Fatalf("semanticqueue unsafe_payload must be an unsafe replay class")
	}
}

func TestUnsafeReplayRefusalGivesActionableGuidance(t *testing.T) {
	guidance, ok := unsafeReplayRefusal("input_invalid")
	if !ok || guidance == "" {
		t.Fatalf("expected actionable guidance for input_invalid, got %q ok=%v", guidance, ok)
	}
	if _, ok := unsafeReplayRefusal("dependency_unavailable"); ok {
		t.Fatalf("dependency_unavailable is retryable and must not be refused")
	}
	if _, ok := unsafeReplayRefusal(""); ok {
		t.Fatalf("empty class must be treated as safe (broad selector)")
	}
}

func TestReplayRequestFingerprintStableAndSelectorSensitive(t *testing.T) {
	a := replayRequestFingerprint([]string{"b", "a"}, "scope-1", "reducer", "", 100, false)
	b := replayRequestFingerprint([]string{"a", "b"}, "scope-1", "reducer", "", 100, false)
	if a != b {
		t.Fatalf("fingerprint must be order-independent: %q != %q", a, b)
	}
	if a == replayRequestFingerprint([]string{"a", "b"}, "scope-2", "reducer", "", 100, false) {
		t.Fatalf("fingerprint must change when scope changes")
	}
	if a == replayRequestFingerprint([]string{"a", "b"}, "scope-1", "reducer", "", 100, true) {
		t.Fatalf("fingerprint must change when force changes")
	}
	if a == replayRequestFingerprint([]string{"a", "b"}, "scope-1", "reducer", "", 1, false) {
		t.Fatalf("fingerprint must change when limit changes")
	}
}

func TestUnsafeReplayFailureClassListSorted(t *testing.T) {
	list := unsafeReplayFailureClassList()
	if len(list) != 2 {
		t.Fatalf("expected 2 unsafe classes, got %d: %v", len(list), list)
	}
	if list[0] > list[1] {
		t.Fatalf("unsafe class list must be sorted: %v", list)
	}
}
