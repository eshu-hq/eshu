// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
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

func TestGenerationBuildEmitsTagObservationForLabeledResources(t *testing.T) {
	key := testRedactionKey(t)
	gen := NewGeneration(testGenerationBoundary(), key)

	labeled := testResourceObservation()
	labeled.Labels = map[string]string{"env": "prod", "owner": "platform-owner"}
	labeled.LabelFingerprint = map[string]string{"env": "", "owner": ""}
	labeled.SourceRecordID = "assets/vm-1"
	labeled.SourceURI = "cai://assets/list/projects/my-project"

	unlabeled := testResourceObservation()
	unlabeled.Name = "//storage.googleapis.com/projects/_/buckets/my-bucket"
	unlabeled.AssetType = "storage.googleapis.com/Bucket"
	unlabeled.DisplayName = "my-bucket"
	unlabeled.Labels = nil
	unlabeled.LabelFingerprint = nil
	unlabeled.SourceRecordID = "assets/my-bucket"

	if err := gen.AddPage([]ResourceObservation{labeled, unlabeled}); err != nil {
		t.Fatalf("AddPage: %v", err)
	}

	envelopes, err := gen.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := countKind(envelopes, facts.GCPCloudResourceFactKind); got != 2 {
		t.Fatalf("resource fact count = %d, want 2", got)
	}
	if got := countKind(envelopes, facts.GCPTagObservationFactKind); got != 1 {
		t.Fatalf("tag fact count = %d, want 1", got)
	}

	var tag facts.Envelope
	for _, env := range envelopes {
		if payload := fmt.Sprintf("%#v", env.Payload); strings.Contains(payload, "prod") || strings.Contains(payload, "platform-owner") {
			t.Fatalf("raw label value leaked in %s payload: %s", env.FactKind, payload)
		}
		if env.FactKind == facts.GCPTagObservationFactKind {
			tag = env
		}
	}
	if tag.FactKind == "" {
		t.Fatal("tag observation envelope missing")
	}
	if tag.Payload["full_resource_name"] != labeled.Name {
		t.Fatalf("tag full_resource_name = %v, want %s", tag.Payload["full_resource_name"], labeled.Name)
	}
	if tag.Payload["source_kind"] != "label" {
		t.Fatalf("tag source_kind = %v, want label", tag.Payload["source_kind"])
	}
	fingerprints, ok := tag.Payload["tag_value_fingerprints"].(map[string]string)
	if !ok || len(fingerprints) != 2 {
		t.Fatalf("tag_value_fingerprints = %#v, want two fingerprints", tag.Payload["tag_value_fingerprints"])
	}
}

func TestGenerationBuildSkipsTagObservationWithoutRedactionKey(t *testing.T) {
	gen := NewGeneration(testGenerationBoundary(), redact.Key{})
	resource := testResourceObservation()
	resource.Labels = map[string]string{"env": "prod"}

	if err := gen.AddPage([]ResourceObservation{resource}); err != nil {
		t.Fatalf("AddPage: %v", err)
	}
	envelopes, err := gen.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := countKind(envelopes, facts.GCPCloudResourceFactKind); got != 1 {
		t.Fatalf("resource fact count = %d, want 1", got)
	}
	if got := countKind(envelopes, facts.GCPTagObservationFactKind); got != 0 {
		t.Fatalf("tag fact count = %d, want 0", got)
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
	// Duplicate delivery within a generation must dedupe to the same resource
	// and tag-observation stable keys.
	if len(first) != 4 {
		t.Fatalf("expected 4 deduped resource and tag facts, got %d", len(first))
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
