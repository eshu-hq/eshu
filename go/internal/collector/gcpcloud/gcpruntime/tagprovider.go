// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
)

const (
	// TagSourceKindDirect identifies directly attached Resource Manager tags.
	TagSourceKindDirect = "direct"
	// TagSourceKindEffective identifies Resource Manager effective tags.
	TagSourceKindEffective = "effective"

	tagInheritanceDirect    = "direct"
	tagInheritanceInherited = "inherited"
)

// TagRequest identifies one bounded Resource Manager tag-listing page request.
// It carries only source identity and no credential material.
type TagRequest struct {
	Scope            ScopeConfig
	FullResourceName string
	AssetType        string
	SourceKind       string
	PageToken        string
}

// TagPage is one parsed Resource Manager tag response page. Tags maps preserved
// tag key -> raw tag value; Source fingerprints values before facts are emitted.
type TagPage struct {
	Tags             map[string]string
	InheritanceState map[string]string
	NextPageToken    string
	SourceURI        string
}

// TagProvider is the optional seam for direct/effective GCP Resource Manager
// tag APIs. It is called only when the scope explicitly opts in.
type TagProvider interface {
	FetchTagPage(ctx context.Context, req TagRequest) (TagPage, error)
}

func tagSourceKinds(scopeCfg ScopeConfig) []string {
	kinds := make([]string, 0, 2)
	if scopeCfg.DirectTagsEnabled {
		kinds = append(kinds, TagSourceKindDirect)
	}
	if scopeCfg.EffectiveTagsEnabled {
		kinds = append(kinds, TagSourceKindEffective)
	}
	return kinds
}

func tagObservationFromPage(
	boundary gcpcloud.Boundary,
	resource gcpcloud.ResourceObservation,
	sourceKind string,
	page TagPage,
) (gcpcloud.TagObservation, bool) {
	if len(page.Tags) == 0 {
		return gcpcloud.TagObservation{}, false
	}
	return gcpcloud.TagObservation{
		Boundary:         boundary,
		FullResourceName: resource.Name,
		AssetType:        resource.AssetType,
		Tags:             page.Tags,
		SourceKind:       strings.TrimSpace(sourceKind),
		InheritanceState: page.InheritanceState,
		UpdateTime:       boundary.ObservedAt,
		SourceRecordID:   tagSourceRecordID(resource.SourceRecordID, sourceKind),
		SourceURI:        page.SourceURI,
	}, true
}

func tagSourceRecordID(resourceRecordID, sourceKind string) string {
	resourceRecordID = strings.TrimSpace(resourceRecordID)
	sourceKind = strings.TrimSpace(sourceKind)
	if resourceRecordID == "" {
		return sourceKind
	}
	if sourceKind == "" {
		return resourceRecordID
	}
	return resourceRecordID + "#" + sourceKind
}
