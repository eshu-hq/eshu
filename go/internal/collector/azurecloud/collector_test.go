// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// fixturePageProvider serves pre-parsed Resource Graph pages keyed by skip
// token, modeling the provider's $skipToken pagination without live calls. The
// empty string keys the first page.
type fixturePageProvider struct {
	pages       map[string]ResourceGraphPage
	changePages map[string]ResourceChangesPage
	calls       []string
	failOn      string
	failErr     error
	scopeErr    *ScopeAccess
}

func (p *fixturePageProvider) NextPage(_ context.Context, skipToken string) (ResourceGraphPage, error) {
	p.calls = append(p.calls, skipToken)
	if p.failOn == skipToken && p.failErr != nil {
		return ResourceGraphPage{}, p.failErr
	}
	page, ok := p.pages[skipToken]
	if !ok {
		return ResourceGraphPage{}, errors.New("no fixture page for skip token")
	}
	return page, nil
}

func (p *fixturePageProvider) NextResourceChangesPage(
	_ context.Context,
	skipToken string,
) (ResourceChangesPage, error) {
	p.calls = append(p.calls, skipToken)
	if p.failOn == skipToken && p.failErr != nil {
		return ResourceChangesPage{}, p.failErr
	}
	page, ok := p.changePages[skipToken]
	if !ok {
		return ResourceChangesPage{}, errors.New("no fixture resource changes page for skip token")
	}
	return page, nil
}

func (p *fixturePageProvider) ScopeAccess(context.Context) ScopeAccess {
	if p.scopeErr != nil {
		return *p.scopeErr
	}
	return ScopeAccess{}
}

func newTwoPageProvider(t *testing.T) *fixturePageProvider {
	t.Helper()
	page1, err := ParseResourceGraphPage(loadFixture(t, "resources_page1.json"))
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	page2, err := ParseResourceGraphPage(loadFixture(t, "resources_page2.json"))
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	return &fixturePageProvider{
		pages: map[string]ResourceGraphPage{
			"":                  page1,
			"skip-token-page-2": page2,
		},
	}
}

func collectFacts(t *testing.T, result ScanResult) []facts.Envelope {
	t.Helper()
	return result.Facts
}

func TestCollectPaginationResume(t *testing.T) {
	provider := newTwoPageProvider(t)
	collector := NewCollector(provider, nil)
	result, err := collector.Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}
	// Two skip tokens visited: "" then "skip-token-page-2".
	if len(provider.calls) != 2 {
		t.Fatalf("provider visited %d pages, want 2: %v", len(provider.calls), provider.calls)
	}
	if provider.calls[1] != "skip-token-page-2" {
		t.Fatalf("second call skip token = %q, want skip-token-page-2", provider.calls[1])
	}
	resources := factsOfKind(result.Facts, facts.AzureCloudResourceFactKind)
	if len(resources) != 3 {
		t.Fatalf("emitted %d resource facts, want 3", len(resources))
	}
	if result.ResourceCount != 3 {
		t.Fatalf("ResourceCount = %d, want 3", result.ResourceCount)
	}
	if result.PageCount != 2 {
		t.Fatalf("PageCount = %d, want 2", result.PageCount)
	}
	if result.SkipTokenResumes != 1 {
		t.Fatalf("SkipTokenResumes = %d, want 1", result.SkipTokenResumes)
	}
}

func TestCollectIdempotentReEmission(t *testing.T) {
	first, err := NewCollector(newTwoPageProvider(t), nil).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("first collect: %v", err)
	}
	second, err := NewCollector(newTwoPageProvider(t), nil).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("second collect: %v", err)
	}
	firstKeys := stableKeySet(collectFacts(t, first))
	secondKeys := stableKeySet(collectFacts(t, second))
	if len(firstKeys) != len(secondKeys) {
		t.Fatalf("stable key count differs: %d vs %d", len(firstKeys), len(secondKeys))
	}
	for key := range firstKeys {
		if _, ok := secondKeys[key]; !ok {
			t.Fatalf("stable key %q missing on re-emission", key)
		}
	}
	// Fact IDs must also converge for the same generation.
	firstIDs := factIDSet(collectFacts(t, first))
	for _, env := range collectFacts(t, second) {
		if _, ok := firstIDs[env.FactID]; !ok {
			t.Fatalf("fact id %q not stable across generations", env.FactID)
		}
	}
}

func TestCollectStaleGenerationRejected(t *testing.T) {
	provider := newTwoPageProvider(t)
	collector := NewCollector(provider, nil)
	boundary := testBoundary()
	boundary.GenerationID = ""
	if _, err := collector.Collect(context.Background(), boundary); err == nil {
		t.Fatal("expected error for empty generation id")
	}

	boundary = testBoundary()
	boundary.FencingToken = 0
	if _, err := collector.Collect(context.Background(), boundary); err == nil {
		t.Fatal("expected error for non-positive fencing token")
	}
}

func TestCollectTruncationEmitsWarning(t *testing.T) {
	page, err := ParseResourceGraphPage(loadFixture(t, "resources_truncated.json"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	provider := &fixturePageProvider{pages: map[string]ResourceGraphPage{"": page}}
	result, err := NewCollector(provider, nil).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	warnings := factsOfKind(result.Facts, facts.AzureCollectionWarningFactKind)
	if len(warnings) != 1 {
		t.Fatalf("emitted %d warnings, want 1", len(warnings))
	}
	if warnings[0].Payload["warning_kind"] != WarningResultTruncated {
		t.Fatalf("warning kind = %v, want %s", warnings[0].Payload["warning_kind"], WarningResultTruncated)
	}
	if result.Truncated != true {
		t.Fatal("result Truncated flag should be true")
	}
}

func TestCollectPartialScopeEmitsWarning(t *testing.T) {
	provider := newTwoPageProvider(t)
	provider.scopeErr = &ScopeAccess{
		Partial:             true,
		HiddenResourceCount: 4,
		Reason:              WarningPermissionHidden,
		Message:             "subscription 22222222 not readable",
	}
	result, err := NewCollector(provider, nil).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	warnings := factsOfKind(result.Facts, facts.AzureCollectionWarningFactKind)
	if len(warnings) != 1 {
		t.Fatalf("emitted %d warnings, want 1", len(warnings))
	}
	w := warnings[0]
	if w.Payload["warning_kind"] != WarningPermissionHidden {
		t.Fatalf("warning kind = %v, want %s", w.Payload["warning_kind"], WarningPermissionHidden)
	}
	if w.Payload["outcome"] != OutcomePartial {
		t.Fatalf("outcome = %v", w.Payload["outcome"])
	}
	if w.Payload["hidden_resource_count"] != 4 {
		t.Fatalf("hidden_resource_count = %v", w.Payload["hidden_resource_count"])
	}
	if !result.Partial {
		t.Fatal("result Partial flag should be true")
	}
}

func TestCollectPartialScopeDefaultsReason(t *testing.T) {
	provider := newTwoPageProvider(t)
	provider.scopeErr = &ScopeAccess{Partial: true, HiddenResourceCount: 1}
	result, err := NewCollector(provider, nil).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	warnings := factsOfKind(result.Facts, facts.AzureCollectionWarningFactKind)
	if len(warnings) != 1 {
		t.Fatalf("emitted %d warnings, want 1", len(warnings))
	}
	if warnings[0].Payload["warning_kind"] != WarningPartialScope {
		t.Fatalf("default warning kind = %v, want %s", warnings[0].Payload["warning_kind"], WarningPartialScope)
	}
}

func TestCollectRedactsExtensionPayloads(t *testing.T) {
	result, err := NewCollector(newTwoPageProvider(t), nil).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, env := range factsOfKind(result.Facts, facts.AzureCloudResourceFactKind) {
		ext := env.Payload["extension"].(map[string]any)
		data := ext["data"].(map[string]any)
		assertNoForbiddenKeys(t, data)
	}
}

func TestCollectMalformedRowEmitsUnsupportedWarning(t *testing.T) {
	page, err := ParseResourceGraphPage(loadFixture(t, "resources_malformed_id.json"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	provider := &fixturePageProvider{pages: map[string]ResourceGraphPage{"": page}}
	result, err := NewCollector(provider, nil).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("collect should not fail on a malformed row: %v", err)
	}
	if result.ResourceCount != 1 {
		t.Fatalf("ResourceCount = %d, want 1 (only the valid row)", result.ResourceCount)
	}
	warnings := factsOfKind(result.Facts, facts.AzureCollectionWarningFactKind)
	if len(warnings) != 1 {
		t.Fatalf("emitted %d warnings, want 1", len(warnings))
	}
	if warnings[0].Payload["warning_kind"] != WarningUnsupported {
		t.Fatalf("warning kind = %v, want %s", warnings[0].Payload["warning_kind"], WarningUnsupported)
	}
}

func TestCollectPropagatesPageError(t *testing.T) {
	provider := newTwoPageProvider(t)
	provider.failOn = "skip-token-page-2"
	provider.failErr = errors.New("resource graph throttled")
	if _, err := NewCollector(provider, nil).Collect(context.Background(), testBoundary()); err == nil {
		t.Fatal("expected error to propagate from failing page")
	}
}

func factsOfKind(envs []facts.Envelope, kind string) []facts.Envelope {
	var out []facts.Envelope
	for _, env := range envs {
		if env.FactKind == kind {
			out = append(out, env)
		}
	}
	return out
}

func stableKeySet(envs []facts.Envelope) map[string]struct{} {
	set := make(map[string]struct{}, len(envs))
	for _, env := range envs {
		set[env.StableFactKey] = struct{}{}
	}
	return set
}

func factIDSet(envs []facts.Envelope) map[string]struct{} {
	set := make(map[string]struct{}, len(envs))
	for _, env := range envs {
		set[env.FactID] = struct{}{}
	}
	return set
}

func assertNoForbiddenKeys(t *testing.T, data map[string]any) {
	t.Helper()
	for key, value := range data {
		if keyIsForbidden(key) {
			t.Fatalf("forbidden key %q present in emitted extension", key)
		}
		if nested, ok := value.(map[string]any); ok {
			assertNoForbiddenKeys(t, nested)
		}
	}
}
