// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

func serviceStorySupportTargetRefs(filter serviceStoryTargetSupportFilter) []documentationTargetRef {
	scope := documentationTargetScopeFromValues(
		filter.Repository,
		filter.TargetKind,
		filter.TargetID,
		filter.ServiceID,
	)
	baseRefs := documentationTargetRefs(scope)
	refs := make([]documentationTargetRef, 0, len(baseRefs)*2)
	for _, ref := range baseRefs {
		refs = append(refs, serviceStorySupportTargetRefAliases(ref)...)
	}
	return uniqueDocumentationTargetRefs(refs)
}

func serviceStorySupportTargetRefAliases(ref documentationTargetRef) []documentationTargetRef {
	ref.kind = strings.TrimSpace(ref.kind)
	ref.id = strings.TrimSpace(ref.id)
	if ref.id == "" {
		return nil
	}
	switch strings.ToLower(ref.kind) {
	case "service", "workload":
		return []documentationTargetRef{
			{kind: "service", id: ref.id},
			{kind: "workload", id: ref.id},
			{kind: "Service", id: ref.id},
			{kind: "Workload", id: ref.id},
		}
	case "repository", "repo":
		return []documentationTargetRef{
			{kind: "repository", id: ref.id},
			{kind: "repo", id: ref.id},
			{kind: "Repository", id: ref.id},
			{kind: "Repo", id: ref.id},
		}
	default:
		return []documentationTargetRef{ref}
	}
}

func serviceStorySupportPayloadMatchesTargetRefs(payload map[string]any, refs []documentationTargetRef) bool {
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

func serviceStorySupportPayloadMatchesTargetRef(payload map[string]any, ref documentationTargetRef) bool {
	return serviceStorySupportRefListMatchesTarget(payload["candidate_refs"], ref, "kind", "id") ||
		serviceStorySupportRefListMatchesTarget(payload["evidence_refs"], ref, "kind", "id") ||
		serviceStorySupportRefListMatchesTarget(payload["linked_entities"], ref, "entity_type", "entity_id")
}

func serviceStorySupportRefListMatchesTarget(raw any, ref documentationTargetRef, kindKey, idKey string) bool {
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

func serviceStorySupportRefObjectMatchesTarget(raw any, ref documentationTargetRef, kindKey, idKey string) bool {
	value, _ := raw.(map[string]any)
	if len(value) == 0 {
		return false
	}
	id := strings.TrimSpace(documentationStringAny(value[idKey]))
	if id == "" || id != ref.id {
		return false
	}
	if ref.kind == "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(documentationStringAny(value[kindKey])), ref.kind)
}

func serviceStorySupportStringRefObjectMatchesTarget(
	value map[string]string,
	ref documentationTargetRef,
	kindKey string,
	idKey string,
) bool {
	id := strings.TrimSpace(value[idKey])
	if id == "" || id != ref.id {
		return false
	}
	if ref.kind == "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(value[kindKey]), ref.kind)
}
