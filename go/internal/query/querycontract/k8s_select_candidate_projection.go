// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "strings"

// K8sNamespace reads the namespace out of a content entity's metadata and
// trims it.
//
// Namespace equality gates SELECTS matching, so every path that derives a
// namespace has to derive it the same way. The trim lives here, called by both
// the match-input adapter in package query and K8sSelectCandidateFromEntity
// below, rather than being written out twice where the two copies could drift
// apart without anything failing.
func K8sNamespace(metadata map[string]any) string {
	value, _ := metadata["namespace"].(string)
	return strings.TrimSpace(value)
}

// K8sSelectCandidateFromEntity projects an EntityContent into the narrow
// K8sSelectCandidate.
//
// It is the in-memory equivalent of the ListRepoK8sSelectCandidates SQL
// projection. The comma-ok reads on selector and pod_template_labels are what
// preserve the tri-state the matcher depends on: a key that is absent and a
// key holding a present-but-empty string are different answers, and a plain
// type assertion would collapse them into one. The SQL side gets the same
// tri-state from jsonb_typeof(...) = 'string'.
//
// It lives in this package so a ContentStore double holding EntityContent rows
// projects them exactly as the production narrow fetch does, from outside
// package query (#6060).
func K8sSelectCandidateFromEntity(entity EntityContent) K8sSelectCandidate {
	kind, _ := entity.Metadata["kind"].(string)
	selector, selectorPresent := entity.Metadata["selector"].(string)
	podTemplateLabels, podTemplateLabelsPresent := entity.Metadata["pod_template_labels"].(string)
	return K8sSelectCandidate{
		EntityID:                 entity.EntityID,
		EntityName:               entity.EntityName,
		Kind:                     kind,
		Namespace:                K8sNamespace(entity.Metadata),
		Selector:                 selector,
		SelectorPresent:          selectorPresent,
		PodTemplateLabels:        podTemplateLabels,
		PodTemplateLabelsPresent: podTemplateLabelsPresent,
	}
}
