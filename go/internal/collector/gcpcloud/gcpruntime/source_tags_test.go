// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSourceEmitsOptInDirectAndEffectiveTagFacts(t *testing.T) {
	scopeCfg := testScope()
	scopeCfg.DirectTagsEnabled = true
	scopeCfg.EffectiveTagsEnabled = true
	resolved := scopeCfg.withDefaults()
	resource := gcpcloud.ResourceObservation{
		Name:           "//compute.googleapis.com/projects/sanitized-project/zones/us-central1-a/instances/api-1",
		AssetType:      "compute.googleapis.com/Instance",
		SourceRecordID: "assets/api-1",
	}
	provider := NewFixturePageProvider(map[string][]gcpcloud.AssetsListPage{
		resolved.ScopeID: {
			{Resources: []gcpcloud.ResourceObservation{resource}},
		},
	})
	tagProvider := &recordingTagProvider{
		pages: map[string][]TagPage{
			tagProviderKey(resource.Name, TagSourceKindDirect): {
				{Tags: map[string]string{"123/env": "prod"}, SourceURI: "resourcemanager://tagBindings.list"},
			},
			tagProviderKey(resource.Name, TagSourceKindEffective): {
				{
					Tags:             map[string]string{"123/env": "prod", "123/team": "platform"},
					InheritanceState: map[string]string{"123/env": "direct", "123/team": "inherited"},
					SourceURI:        "resourcemanager://effectiveTags.list",
				},
			},
		},
	}
	src := newSource(t, testConfig(scopeCfg), provider, nil)
	src.TagProvider = tagProvider

	collected, ok, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if !ok {
		t.Fatal("Next returned ok=false, want a generation")
	}
	envs := drainFacts(t, collected)
	if got := countKind(envs, facts.GCPTagObservationFactKind); got != 2 {
		t.Fatalf("tag fact count = %d, want 2 direct/effective facts", got)
	}
	seen := map[string]facts.Envelope{}
	for _, env := range envs {
		if env.FactKind != facts.GCPTagObservationFactKind {
			continue
		}
		payloadText := stringify(env.Payload)
		if containsAny(payloadText, "prod", "platform") {
			t.Fatalf("raw tag value leaked in payload: %s", payloadText)
		}
		seen[stringify(env.Payload["source_kind"])] = env
	}
	if _, ok := seen[TagSourceKindDirect]; !ok {
		t.Fatalf("direct tag fact missing: %#v", seen)
	}
	effective := seen[TagSourceKindEffective]
	if effective.FactKind == "" {
		t.Fatalf("effective tag fact missing: %#v", seen)
	}
	state, ok := effective.Payload["tag_inheritance_state"].(map[string]string)
	if !ok || state["123/team"] != "inherited" {
		t.Fatalf("effective tag inheritance state = %#v", effective.Payload["tag_inheritance_state"])
	}
	if len(tagProvider.requests) != 2 {
		t.Fatalf("tag requests = %d, want 2", len(tagProvider.requests))
	}
}

func TestSourceTagProviderWarningBecomesCollectionWarning(t *testing.T) {
	scopeCfg := testScope()
	scopeCfg.DirectTagsEnabled = true
	resolved := scopeCfg.withDefaults()
	resource := gcpcloud.ResourceObservation{
		Name:      "//compute.googleapis.com/projects/sanitized-project/zones/us-central1-a/instances/api-1",
		AssetType: "compute.googleapis.com/Instance",
	}
	provider := NewFixturePageProvider(map[string][]gcpcloud.AssetsListPage{
		resolved.ScopeID: {
			{Resources: []gcpcloud.ResourceObservation{resource}},
		},
	})
	src := newSource(t, testConfig(scopeCfg), provider, nil)
	src.TagProvider = warningTagProvider{warning: ProviderWarning{
		WarningKind: gcpcloud.WarningKindPartialPermission,
		Outcome:     gcpcloud.OutcomePartial,
		Reason:      "resource manager tag permission denied",
		SourceURI:   "resourcemanager://tagBindings.list",
	}}

	collected, ok, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if !ok {
		t.Fatal("Next returned ok=false, want a generation")
	}
	envs := drainFacts(t, collected)
	if got := countKind(envs, facts.GCPCollectionWarningFactKind); got != 1 {
		t.Fatalf("warning fact count = %d, want 1", got)
	}
	warning := firstEnvelopeKind(t, envs, facts.GCPCollectionWarningFactKind)
	if got := warning.Payload["warning_kind"]; got != gcpcloud.WarningKindPartialPermission {
		t.Fatalf("warning_kind = %v, want partial_permission", got)
	}
	if got := warning.SourceRef.SourceURI; got != "resourcemanager://tagBindings.list" {
		t.Fatalf("source_uri = %v, want tagBindings source", got)
	}
}

func TestSourceMergesPaginatedTagPagesIntoOneFact(t *testing.T) {
	scopeCfg := testScope()
	scopeCfg.EffectiveTagsEnabled = true
	resolved := scopeCfg.withDefaults()
	resource := gcpcloud.ResourceObservation{
		Name:           "//compute.googleapis.com/projects/sanitized-project/zones/us-central1-a/instances/api-1",
		AssetType:      "compute.googleapis.com/Instance",
		SourceRecordID: "assets/api-1",
	}
	provider := NewFixturePageProvider(map[string][]gcpcloud.AssetsListPage{
		resolved.ScopeID: {
			{Resources: []gcpcloud.ResourceObservation{resource}},
		},
	})
	tagProvider := &recordingTagProvider{
		pages: map[string][]TagPage{
			tagProviderKey(resource.Name, TagSourceKindEffective): {
				{
					Tags:             map[string]string{"123/env": "prod"},
					InheritanceState: map[string]string{"123/env": "direct"},
					NextPageToken:    "PAGE2",
					SourceURI:        "resourcemanager://effectiveTags.list",
				},
				{
					Tags:             map[string]string{"123/team": "platform"},
					InheritanceState: map[string]string{"123/team": "inherited"},
					SourceURI:        "resourcemanager://effectiveTags.list",
				},
			},
		},
	}
	src := newSource(t, testConfig(scopeCfg), provider, nil)
	src.TagProvider = tagProvider

	collected, ok, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if !ok {
		t.Fatal("Next returned ok=false, want a generation")
	}
	envs := drainFacts(t, collected)
	if got := countKind(envs, facts.GCPTagObservationFactKind); got != 1 {
		t.Fatalf("tag fact count = %d, want one merged effective fact", got)
	}
	tag := firstEnvelopeKind(t, envs, facts.GCPTagObservationFactKind)
	fingerprints, ok := tag.Payload["tag_value_fingerprints"].(map[string]string)
	if !ok || len(fingerprints) != 2 {
		t.Fatalf("tag_value_fingerprints = %#v, want two merged entries", tag.Payload["tag_value_fingerprints"])
	}
	state, ok := tag.Payload["tag_inheritance_state"].(map[string]string)
	if !ok || state["123/team"] != "inherited" || state["123/env"] != "direct" {
		t.Fatalf("tag_inheritance_state = %#v, want merged states", tag.Payload["tag_inheritance_state"])
	}
	if len(tagProvider.requests) != 2 {
		t.Fatalf("tag requests = %d, want 2 pages", len(tagProvider.requests))
	}
	if got := tagProvider.requests[1].PageToken; got != "PAGE2" {
		t.Fatalf("second page token = %q, want PAGE2", got)
	}
}

type recordingTagProvider struct {
	pages    map[string][]TagPage
	requests []TagRequest
}

func (p *recordingTagProvider) FetchTagPage(_ context.Context, req TagRequest) (TagPage, error) {
	p.requests = append(p.requests, req)
	key := tagProviderKey(req.FullResourceName, req.SourceKind)
	pages := p.pages[key]
	if len(pages) == 0 {
		return TagPage{}, ProviderWarning{
			WarningKind: gcpcloud.WarningKindUnsupported,
			Outcome:     gcpcloud.OutcomeUnsupported,
			Reason:      "test tag page missing",
			SourceURI:   "resourcemanager://test",
		}
	}
	if strings.TrimSpace(req.PageToken) == "" {
		return pages[0], nil
	}
	for i, page := range pages {
		if strings.TrimSpace(page.NextPageToken) == strings.TrimSpace(req.PageToken) {
			if i+1 < len(pages) {
				return pages[i+1], nil
			}
			break
		}
	}
	return TagPage{}, ProviderWarning{
		WarningKind: gcpcloud.WarningKindPageTokenExpired,
		Outcome:     gcpcloud.OutcomePartial,
		Reason:      "test tag page token expired",
		SourceURI:   "resourcemanager://test",
	}
}

type warningTagProvider struct {
	warning ProviderWarning
}

func (p warningTagProvider) FetchTagPage(context.Context, TagRequest) (TagPage, error) {
	return TagPage{}, p.warning
}

func tagProviderKey(fullResourceName string, sourceKind string) string {
	return strings.TrimSpace(fullResourceName) + "\x00" + strings.TrimSpace(sourceKind)
}

func stringify(v any) string {
	return fmt.Sprintf("%v", v)
}

func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}

var _ TagProvider = (*recordingTagProvider)(nil)
