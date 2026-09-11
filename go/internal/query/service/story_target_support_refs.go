// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func StorySupportTargetRefs(filter querycontract.ServiceStoryTargetSupportFilter) []querycontract.DocumentationTargetRef {
	scope := querycontract.DocumentationTargetScopeFromValues(
		filter.Repository,
		filter.TargetKind,
		filter.TargetID,
		filter.ServiceID,
	)
	baseRefs := querycontract.DocumentationTargetRefs(scope)
	refs := make([]querycontract.DocumentationTargetRef, 0, len(baseRefs)*2)
	for _, ref := range baseRefs {
		refs = append(refs, serviceStorySupportTargetRefAliases(ref)...)
	}
	return querycontract.UniqueDocumentationTargetRefs(refs)
}

func serviceStorySupportTargetRefAliases(ref querycontract.DocumentationTargetRef) []querycontract.DocumentationTargetRef {
	ref.Kind = strings.TrimSpace(ref.Kind)
	ref.ID = strings.TrimSpace(ref.ID)
	if ref.ID == "" {
		return nil
	}
	switch strings.ToLower(ref.Kind) {
	case "service", "workload":
		return []querycontract.DocumentationTargetRef{
			{Kind: "service", ID: ref.ID},
			{Kind: "workload", ID: ref.ID},
			{Kind: "Service", ID: ref.ID},
			{Kind: "Workload", ID: ref.ID},
		}
	case "repository", "repo":
		return []querycontract.DocumentationTargetRef{
			{Kind: "repository", ID: ref.ID},
			{Kind: "repo", ID: ref.ID},
			{Kind: "Repository", ID: ref.ID},
			{Kind: "Repo", ID: ref.ID},
		}
	default:
		return []querycontract.DocumentationTargetRef{ref}
	}
}

func StorySupportPayloadMatchesTargetRefs(payload map[string]any, refs []querycontract.DocumentationTargetRef) bool {
	for _, ref := range refs {
		if serviceStorySupportPayloadMatchesTargetRef(payload, ref) {
			return true
		}
	}
	nested, _ := payload["payload"].(map[string]any)
	if len(nested) == 0 {
		return false
	}
	for _, ref := range refs {
		if serviceStorySupportPayloadMatchesTargetRef(nested, ref) {
			return true
		}
	}
	return false
}

func serviceStorySupportPayloadMatchesTargetRef(payload map[string]any, ref querycontract.DocumentationTargetRef) bool {
	return serviceStorySupportRefListMatchesTarget(payload["candidate_refs"], ref, "kind", "id") ||
		serviceStorySupportRefListMatchesTarget(payload["evidence_refs"], ref, "kind", "id") ||
		serviceStorySupportRefListMatchesTarget(payload["linked_entities"], ref, "entity_type", "entity_id")
}

func serviceStorySupportRefListMatchesTarget(raw any, ref querycontract.DocumentationTargetRef, kindKey, idKey string) bool {
	switch values := raw.(type) {
	case []any:
		for _, value := range values {
			if serviceStorySupportRefObjectMatchesTarget(value, ref, kindKey, idKey) {
				return true
			}
		}
	case []map[string]any:
		for _, value := range values {
			if serviceStorySupportRefObjectMatchesTarget(value, ref, kindKey, idKey) {
				return true
			}
		}
	case []map[string]string:
		for _, value := range values {
			if serviceStorySupportStringRefObjectMatchesTarget(value, ref, kindKey, idKey) {
				return true
			}
		}
	}
	return false
}

func serviceStorySupportRefObjectMatchesTarget(raw any, ref querycontract.DocumentationTargetRef, kindKey, idKey string) bool {
	value, _ := raw.(map[string]any)
	if len(value) == 0 {
		return false
	}
	id := strings.TrimSpace(querycontract.DocumentationStringAny(value[idKey]))
	if id == "" || id != ref.ID {
		return false
	}
	if ref.Kind == "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(querycontract.DocumentationStringAny(value[kindKey])), ref.Kind)
}

func serviceStorySupportStringRefObjectMatchesTarget(
	value map[string]string,
	ref querycontract.DocumentationTargetRef,
	kindKey string,
	idKey string,
) bool {
	id := strings.TrimSpace(value[idKey])
	if id == "" || id != ref.ID {
		return false
	}
	if ref.Kind == "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(value[kindKey]), ref.Kind)
}
