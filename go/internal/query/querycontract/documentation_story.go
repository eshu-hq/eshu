// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"fmt"
	"strings"
)

// DocumentationTargetRef names one documentation target by kind and id.
// The fields are exported because staying documentation stayers construct
// and read refs across the package boundary.
type DocumentationTargetRef struct {
	Kind string
	ID   string
}

// DocumentationFindingsStore is the narrow optional port a ContentStore
// implements to answer documentation-findings reads directly. It lives here
// (promoted from root package query for #6060 lane B B3) so the moved
// repository handler family can assert a store against it without importing
// root; root keeps its wider documentationReadModelStore, which satisfies
// this port structurally.
type DocumentationFindingsStore interface {
	DocumentationFindings(context.Context, DocumentationFindingFilter) (DocumentationFindingListReadModel, error)
}

func DocumentationTargetScopeHasSelector(s DocumentationTargetScope) bool {
	return strings.TrimSpace(s.Repository) != "" ||
		strings.TrimSpace(s.TargetID) != "" ||
		strings.TrimSpace(s.ServiceID) != ""
}

// DocumentationTargetScopeFromFactFilter selects the documentation target
// scope for one fact listing. It mirrors the finding-filter variant for
// #6060 so root stayers can build fact-filter refs without importing root.
func DocumentationTargetScopeFromFactFilter(filter DocumentationFactFilter) DocumentationTargetScope {
	return DocumentationTargetScopeFromValues(
		filter.Repository,
		filter.TargetKind,
		filter.TargetID,
		filter.ServiceID,
	)
}

func DocumentationTargetScopeFromFindingFilter(filter DocumentationFindingFilter) DocumentationTargetScope {
	return DocumentationTargetScopeFromValues(
		filter.Repository,
		filter.TargetKind,
		filter.TargetID,
		filter.ServiceID,
	)
}

func DocumentationTargetScopeFromValues(repository, targetKind, targetID, serviceID string) DocumentationTargetScope {
	scope := DocumentationTargetScope{
		Repository: strings.TrimSpace(repository),
		ServiceID:  strings.TrimSpace(serviceID),
	}
	targetKind = strings.TrimSpace(targetKind)
	targetID = strings.TrimSpace(targetID)
	if scope.ServiceID != "" {
		scope.TargetKind = "service"
		scope.TargetID = scope.ServiceID
	}
	if scope.TargetID == "" && targetID != "" {
		scope.TargetKind = targetKind
		scope.TargetID = targetID
	}
	if scope.TargetID == "" && scope.Repository != "" &&
		(targetKind == "" || targetKind == "repository") {
		scope.TargetKind = "repository"
		scope.TargetID = scope.Repository
	}
	return scope
}

func DocumentationTargetRefsFromFindingFilter(filter DocumentationFindingFilter) []DocumentationTargetRef {
	return DocumentationTargetRefs(DocumentationTargetScopeFromFindingFilter(filter))
}

// DocumentationTargetRefsFromFactFilter selects the documentation target refs
// for one fact listing. It lives here for #6060 so root stayers outside the
// moved families can name it without importing root.
func DocumentationTargetRefsFromFactFilter(filter DocumentationFactFilter) []DocumentationTargetRef {
	return DocumentationTargetRefs(DocumentationTargetScopeFromFactFilter(filter))
}

func DocumentationTargetRefs(scope DocumentationTargetScope) []DocumentationTargetRef {
	refs := []DocumentationTargetRef{}
	if scope.ServiceID != "" {
		refs = append(refs, DocumentationTargetRef{Kind: "service", ID: scope.ServiceID})
	}
	if scope.TargetID != "" {
		refs = append(refs, DocumentationTargetRef{Kind: scope.TargetKind, ID: scope.TargetID})
	}
	if len(refs) == 0 && scope.Repository != "" {
		refs = append(refs, DocumentationTargetRef{Kind: "repository", ID: scope.Repository})
	}
	return UniqueDocumentationTargetRefs(refs)
}

func UniqueDocumentationTargetRefs(refs []DocumentationTargetRef) []DocumentationTargetRef {
	seen := map[string]struct{}{}
	out := make([]DocumentationTargetRef, 0, len(refs))
	for _, ref := range refs {
		ref.Kind = strings.TrimSpace(ref.Kind)
		ref.ID = strings.TrimSpace(ref.ID)
		if ref.ID == "" {
			continue
		}
		key := ref.Kind + "\x00" + ref.ID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func DocumentationFindingFilterHasExplicitTarget(filter DocumentationFindingFilter) bool {
	return strings.TrimSpace(filter.TargetID) != "" || strings.TrimSpace(filter.ServiceID) != ""
}

func DocumentationPayloadMatchesTargetRefs(payload map[string]any, refs []DocumentationTargetRef) bool {
	for _, ref := range refs {
		if documentationPayloadMatchesTargetRef(payload, ref) {
			return true
		}
	}
	nested, _ := payload["payload"].(map[string]any)
	if len(nested) == 0 {
		return false
	}
	for _, ref := range refs {
		if documentationPayloadMatchesTargetRef(nested, ref) {
			return true
		}
	}
	return false
}

func DocumentationPayloadMatchesTargetRef(payload map[string]any, ref DocumentationTargetRef) bool {
	return documentationRefListMatchesTarget(payload["candidate_refs"], ref, "kind", "id") ||
		documentationRefListMatchesTarget(payload["evidence_refs"], ref, "kind", "id") ||
		documentationRefListMatchesTarget(payload["linked_entities"], ref, "entity_type", "entity_id")
}

func documentationRefListMatchesTarget(raw any, ref DocumentationTargetRef, kindKey, idKey string) bool {
	switch values := raw.(type) {
	case []any:
		for _, value := range values {
			if documentationRefObjectMatchesTarget(value, ref, kindKey, idKey) {
				return true
			}
		}
	case []map[string]any:
		for _, value := range values {
			if documentationRefObjectMatchesTarget(value, ref, kindKey, idKey) {
				return true
			}
		}
	case []map[string]string:
		for _, value := range values {
			if documentationStringRefObjectMatchesTarget(value, ref, kindKey, idKey) {
				return true
			}
		}
	}
	return false
}

func documentationRefObjectMatchesTarget(raw any, ref DocumentationTargetRef, kindKey, idKey string) bool {
	value, _ := raw.(map[string]any)
	if len(value) == 0 {
		return false
	}
	id := strings.TrimSpace(DocumentationStringAny(value[idKey]))
	if id == "" || id != ref.ID {
		return false
	}
	if ref.Kind == "" {
		return true
	}
	return strings.TrimSpace(DocumentationStringAny(value[kindKey])) == ref.Kind
}

func documentationStringRefObjectMatchesTarget(
	value map[string]string,
	ref DocumentationTargetRef,
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
	return strings.TrimSpace(value[kindKey]) == ref.Kind
}

func DocumentationStringAny(raw any) string {
	value, _ := raw.(string)
	return value
}

func DocumentationMissingEvidenceForTarget(coverage DocumentationTargetCoverage) []DocumentationMissingEvidence {
	if !DocumentationTargetScopeHasSelector(coverage.Target) {
		return nil
	}
	if coverage.FindingsReturned > 0 {
		return nil
	}
	if coverage.TargetFactCount > 0 {
		return []DocumentationMissingEvidence{{
			Reason: "documentation_findings_absent",
			Detail: "target documentation facts exist but no admissible documentation findings matched the target scope",
		}}
	}
	if coverage.SourceOnlyCount > 0 {
		return []DocumentationMissingEvidence{{
			Reason: "target_link_not_modeled",
			Detail: "external documentation facts exist, but none carry structured refs for the selected target scope",
		}}
	}
	return []DocumentationMissingEvidence{{
		Reason: "documentation_target_facts_absent",
		Detail: "no collected documentation facts referenced the selected target scope",
	}}
}

const DocumentationStoryReadLimit = 10

func LoadRepositoryStoryTargetDocumentation(
	ctx context.Context,
	content ContentStore,
	repoID string,
) (map[string]any, error) {
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return nil, nil
	}
	return LoadStoryTargetDocumentation(ctx, content, DocumentationFindingFilter{
		Repository: repoID,
		TargetKind: "repository",
		TargetID:   repoID,
		Limit:      DocumentationStoryReadLimit,
	})
}

func LoadStoryTargetDocumentation(
	ctx context.Context,
	content ContentStore,
	filter DocumentationFindingFilter,
) (map[string]any, error) {
	store, ok := content.(DocumentationFindingsStore)
	if !ok || store == nil {
		return nil, nil
	}
	readModel, err := store.DocumentationFindings(ctx, filter)
	if err != nil {
		return nil, err
	}
	return BuildStoryTargetDocumentation(filter, readModel), nil
}

func AttachStoryTargetDocumentation(
	overview map[string]any,
	targetDocumentation map[string]any,
) map[string]any {
	if len(targetDocumentation) == 0 {
		return overview
	}
	if overview == nil {
		overview = map[string]any{}
	}
	overview["target_documentation"] = targetDocumentation
	if story := StoryTargetDocumentationSummary(targetDocumentation); story != "" {
		appendStoryField(overview, story)
	}
	return overview
}

func BuildStoryTargetDocumentation(
	filter DocumentationFindingFilter,
	readModel DocumentationFindingListReadModel,
) map[string]any {
	findings := storyTargetDocumentationFindings(filter, readModel.Findings)
	relatedFacts := readModel.RelatedFacts
	missingEvidence := readModel.MissingEvidence
	if len(missingEvidence) == 0 && DocumentationTargetScopeHasSelector(readModel.Coverage.Target) {
		missingEvidence = DocumentationMissingEvidenceForTarget(readModel.Coverage)
	}
	if len(findings) == 0 && len(relatedFacts) == 0 &&
		readModel.Coverage.TargetFactCount == 0 &&
		readModel.Coverage.SourceOnlyCount == 0 {
		return nil
	}
	if findings == nil {
		findings = []map[string]any{}
	}
	if relatedFacts == nil {
		relatedFacts = []map[string]any{}
	}
	return map[string]any{
		"findings":           findings,
		"finding_count":      len(findings),
		"related_facts":      relatedFacts,
		"related_fact_count": len(relatedFacts),
		"coverage":           documentationTargetCoverageMap(readModel.Coverage),
		"missing_evidence":   documentationMissingEvidenceMaps(missingEvidence),
		"limit":              DocumentationStoryReadLimit,
		"source":             "documentation_read_model",
	}
}

func storyTargetDocumentationFindings(
	filter DocumentationFindingFilter,
	findings []map[string]any,
) []map[string]any {
	if len(findings) == 0 || !DocumentationFindingFilterHasExplicitTarget(filter) {
		return findings
	}
	refs := DocumentationTargetRefsFromFindingFilter(filter)
	if len(refs) == 0 {
		return findings
	}
	filtered := make([]map[string]any, 0, len(findings))
	for _, finding := range findings {
		if DocumentationPayloadMatchesTargetRefs(finding, refs) {
			filtered = append(filtered, finding)
		}
	}
	return filtered
}

func documentationTargetCoverageMap(coverage DocumentationTargetCoverage) map[string]any {
	out := map[string]any{
		"findings_returned": coverage.FindingsReturned,
		"target_fact_count": coverage.TargetFactCount,
		"truncated":         coverage.Truncated,
	}
	if DocumentationTargetScopeHasSelector(coverage.Target) {
		out["target"] = map[string]any{
			"repository":  coverage.Target.Repository,
			"target_kind": coverage.Target.TargetKind,
			"target_id":   coverage.Target.TargetID,
			"service_id":  coverage.Target.ServiceID,
		}
	}
	if len(coverage.TargetFactKinds) > 0 {
		out["target_fact_kinds"] = coverage.TargetFactKinds
	}
	if coverage.SourceOnlyCount > 0 {
		out["source_only_count"] = coverage.SourceOnlyCount
	}
	if len(coverage.SourceOnlyFactKinds) > 0 {
		out["source_only_fact_kinds"] = coverage.SourceOnlyFactKinds
	}
	return out
}

func documentationMissingEvidenceMaps(values []DocumentationMissingEvidence) []map[string]any {
	if len(values) == 0 {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		row := map[string]any{"reason": value.Reason}
		if strings.TrimSpace(value.Detail) != "" {
			row["detail"] = value.Detail
		}
		out = append(out, row)
	}
	return out
}

func StoryTargetDocumentationSummary(targetDocumentation map[string]any) string {
	findingCount := IntVal(targetDocumentation, "finding_count")
	relatedFactCount := IntVal(targetDocumentation, "related_fact_count")
	switch {
	case findingCount > 0:
		return fmt.Sprintf("External documentation includes %d target-linked finding(s).", findingCount)
	case relatedFactCount > 0:
		return fmt.Sprintf("External documentation has %d target-related fact(s) but no admitted finding for this target.", relatedFactCount)
	case IntVal(MapValue(targetDocumentation, "coverage"), "source_only_count") > 0:
		return "External documentation facts exist, but none carry structured refs for this target."
	default:
		return ""
	}
}

func appendStoryField(value map[string]any, fragment string) {
	fragment = strings.TrimSpace(fragment)
	if fragment == "" {
		return
	}
	story := strings.TrimSpace(StringVal(value, "story"))
	if story == "" {
		value["story"] = fragment
		return
	}
	if strings.Contains(story, fragment) {
		return
	}
	value["story"] = story + " " + fragment
}

// documentationPayloadMatchesTargetRef keeps the historical spelling used
// across the documentation story before the #6060 export; new code names
// DocumentationPayloadMatchesTargetRef.
func documentationPayloadMatchesTargetRef(payload map[string]any, ref DocumentationTargetRef) bool {
	return DocumentationPayloadMatchesTargetRef(payload, ref)
}
