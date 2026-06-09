package gcpcloud

import (
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func testGenerationBoundary() Boundary {
	b := testBoundary()
	b.AssetTypeFamily = "mixed"
	return b
}

func TestGenerationPaginationResume(t *testing.T) {
	key := testRedactionKey(t)
	gen := NewGeneration(testGenerationBoundary(), key)

	page1, err := ParseAssetsListPage(readFixture(t, "assets_list_page1.json"))
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if err := gen.AddPage(page1.Resources); err != nil {
		t.Fatalf("AddPage page1: %v", err)
	}
	// Resume from the continuation token by adding the next page.
	if page1.NextPageToken == "" {
		t.Fatal("expected a continuation token on page1")
	}
	page2, err := ParseAssetsListPage(readFixture(t, "assets_list_page2.json"))
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if err := gen.AddPage(page2.Resources); err != nil {
		t.Fatalf("AddPage page2: %v", err)
	}

	envelopes, err := gen.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// 2 resources on page 1 + 1 on page 2 = 3 resource facts.
	if got := countKind(envelopes, "gcp_cloud_resource"); got != 3 {
		t.Fatalf("resource fact count = %d, want 3", got)
	}
	if gen.PageCount() != 2 {
		t.Fatalf("PageCount = %d, want 2", gen.PageCount())
	}
	if gen.ResourceCount() != 3 {
		t.Fatalf("ResourceCount = %d, want 3", gen.ResourceCount())
	}
}

func TestGenerationIdempotentReEmission(t *testing.T) {
	key := testRedactionKey(t)
	page, err := ParseAssetsListPage(readFixture(t, "assets_list_page1.json"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	build := func() []string {
		gen := NewGeneration(testGenerationBoundary(), key)
		if err := gen.AddPage(page.Resources); err != nil {
			t.Fatalf("AddPage: %v", err)
		}
		// Re-add the same page (duplicate delivery) — must converge.
		if err := gen.AddPage(page.Resources); err != nil {
			t.Fatalf("AddPage dup: %v", err)
		}
		envs, err := gen.Build()
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		keys := make([]string, 0, len(envs))
		for _, e := range envs {
			keys = append(keys, e.StableFactKey)
		}
		return keys
	}

	first := build()
	second := build()
	// Duplicate delivery within a generation must dedupe to the same stable keys.
	if len(first) != 2 {
		t.Fatalf("expected 2 deduped resource facts, got %d", len(first))
	}
	if !equalStringSets(first, second) {
		t.Fatalf("re-emission not idempotent: %v vs %v", first, second)
	}
}

func TestGenerationStaleRejection(t *testing.T) {
	tracker := NewGenerationTracker()
	boundary := testGenerationBoundary()

	// Accept generation with fencing token 7.
	if err := tracker.Accept(boundary.ScopeID, boundary.GenerationID, 7); err != nil {
		t.Fatalf("accept first: %v", err)
	}
	// A lower fencing token is stale and must be rejected.
	err := tracker.Accept(boundary.ScopeID, "gen-old", 5)
	if !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("stale accept err = %v, want ErrStaleGeneration", err)
	}
	// Re-accepting the same fencing token (idempotent retry) is allowed.
	if err := tracker.Accept(boundary.ScopeID, boundary.GenerationID, 7); err != nil {
		t.Fatalf("idempotent re-accept: %v", err)
	}
	// A higher fencing token advances the scope.
	if err := tracker.Accept(boundary.ScopeID, "gen-2", 9); err != nil {
		t.Fatalf("advance: %v", err)
	}
	// The previously current token is now stale.
	if err := tracker.Accept(boundary.ScopeID, boundary.GenerationID, 7); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("post-advance stale err = %v, want ErrStaleGeneration", err)
	}
}

func TestGenerationPartialPermissionWarning(t *testing.T) {
	key := testRedactionKey(t)
	gen := NewGeneration(testGenerationBoundary(), key)
	page, _ := ParseAssetsListPage(readFixture(t, "assets_list_page1.json"))
	_ = gen.AddPage(page.Resources)

	gen.AddWarning(WarningObservation{
		Boundary:    testGenerationBoundary(),
		WarningKind: WarningKindPartialPermission,
		Outcome:     OutcomePartial,
		Reason:      "missing roles/cloudasset.viewer",
		HiddenCount: 4,
	})

	envs, err := gen.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := countKind(envs, "gcp_collection_warning"); got != 1 {
		t.Fatalf("warning fact count = %d, want 1", got)
	}
	if gen.WarningCount() != 1 {
		t.Fatalf("WarningCount = %d, want 1", gen.WarningCount())
	}
}

func countKind(envs []facts.Envelope, kind string) int {
	count := 0
	for _, e := range envs {
		if e.FactKind == kind {
			count++
		}
	}
	return count
}

func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]int, len(a))
	for _, v := range a {
		set[v]++
	}
	for _, v := range b {
		set[v]--
		if set[v] < 0 {
			return false
		}
	}
	return true
}
