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

// TestUnsafeReplayClassesIncludeManualReviewTriage proves every dead-letter
// triage class the projector flags manual_review (the durable failure_class an
// operator sees on a dead-lettered row) refuses an un-forced replay. Without
// this, a poison projection_bug or a resource_exhausted item would drain via
// POST /api/v0/admin/replay with no --force, contradicting the triage contract
// documented in internal/projector (#3502, #3514).
func TestUnsafeReplayClassesIncludeManualReviewTriage(t *testing.T) {
	manualReview := projector.ManualReviewTriageClasses()
	if len(manualReview) == 0 {
		t.Fatal("projector must expose at least one manual_review triage class")
	}
	for _, class := range manualReview {
		guidance, unsafe := unsafeReplayRefusal(class)
		if !unsafe {
			t.Fatalf("manual_review triage class %q must refuse an un-forced replay", class)
		}
		if guidance == "" {
			t.Fatalf("manual_review triage class %q must carry actionable refusal guidance", class)
		}
	}

	// The poison bucket is the highest-risk class and must be refused by name.
	if _, unsafe := unsafeReplayRefusal(string(projector.TriageClassProjectionBug)); !unsafe {
		t.Fatalf("projection_bug (poison) must refuse an un-forced replay")
	}
	if _, unsafe := unsafeReplayRefusal(string(projector.TriageClassResourceExhausted)); !unsafe {
		t.Fatalf("resource_exhausted (manual review) must refuse an un-forced replay")
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
	if len(list) < 2 {
		t.Fatalf("expected at least the base unsafe classes, got %d: %v", len(list), list)
	}
	for i := 1; i < len(list); i++ {
		if list[i-1] > list[i] {
			t.Fatalf("unsafe class list must be sorted: %v", list)
		}
		if list[i-1] == list[i] {
			t.Fatalf("unsafe class list must not contain duplicates: %v", list)
		}
	}
	// The base non-retryable/quarantine classes and the manual_review triage
	// classes must all be present in the SQL exclusion list.
	want := map[string]bool{
		string(projector.FailureClassInputInvalid):    false,
		string(semanticqueue.StatusUnsafePayload):      false,
		string(projector.TriageClassProjectionBug):     false,
		string(projector.TriageClassResourceExhausted): false,
	}
	for _, class := range list {
		if _, tracked := want[class]; tracked {
			want[class] = true
		}
	}
	for class, present := range want {
		if !present {
			t.Fatalf("unsafe class list must include %q for SQL exclusion: %v", class, list)
		}
	}
}
