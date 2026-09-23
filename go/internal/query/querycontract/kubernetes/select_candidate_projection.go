// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetes

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Namespace reads the namespace out of a content entity's metadata and
// trims it.
//
// Namespace equality gates SELECTS matching, so every path that derives a
// namespace has to derive it the same way. This helper covers the paths that
// hold a metadata map: the match-input adapter in package query and
// SelectCandidateFromEntity below.
//
// It does not cover all of them. The SQL scan path writes the same
// strings.TrimSpace inline, because it holds a scanned string rather than a
// map and cannot call this signature -- see
// internal/query/content_reader_k8s_select_candidates.go, where
// candidate.Namespace is trimmed directly. The two agree today. They are the
// pair to keep in step, and the earlier claim here that the trim was not
// "written out twice" was wrong: it is.
func Namespace(metadata map[string]any) string {
	value, _ := metadata["namespace"].(string)
	return strings.TrimSpace(value)
}

// SelectCandidateFromEntity projects a querycontract.EntityContent into the
// narrow querycontract.K8sSelectCandidate.
//
// It is the in-memory equivalent of the ListRepoK8sSelectCandidates SQL
// projection. The comma-ok reads on selector and pod_template_labels are what
// preserve the tri-state the matcher depends on: a key that is absent and a
// key holding a present-but-empty string are different answers, and a plain
// type assertion would collapse them into one. The SQL side gets the same
// tri-state from jsonb_typeof(...) = 'string'.
//
// It lives in this package so a ContentStore double holding querycontract.EntityContent rows
// projects them exactly as the production narrow fetch does, from outside
// package query (#6060).
func SelectCandidateFromEntity(entity querycontract.EntityContent) querycontract.K8sSelectCandidate {
	kind, _ := entity.Metadata["kind"].(string)
	selector, selectorPresent := entity.Metadata["selector"].(string)
	podTemplateLabels, podTemplateLabelsPresent := entity.Metadata["pod_template_labels"].(string)
	return querycontract.K8sSelectCandidate{
		EntityID:                 entity.EntityID,
		EntityName:               entity.EntityName,
		Kind:                     kind,
		Namespace:                Namespace(entity.Metadata),
		Selector:                 selector,
		SelectorPresent:          selectorPresent,
		PodTemplateLabels:        podTemplateLabels,
		PodTemplateLabelsPresent: podTemplateLabelsPresent,
	}
}
