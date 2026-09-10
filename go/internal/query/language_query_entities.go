// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querygraphrows"
)

// graphBackedEntityTypes forwards to querycontract.GraphBackedEntityTypes.
// The implementation moved to querycontract for #6060; this alias keeps
// root callers unchanged.
var graphBackedEntityTypes = querycontract.GraphBackedEntityTypes

// contentBackedEntityTypes forwards to querycontract.ContentBackedEntityTypes.
// The implementation moved to querycontract for #6060; this alias keeps
// root callers unchanged.
var contentBackedEntityTypes = querycontract.ContentBackedEntityTypes

// graphFirstContentBackedEntityTypes forwards to
// querycontract.GraphFirstContentBackedEntityTypes. The implementation moved
// to querycontract for #6060; this alias keeps root callers unchanged.
var graphFirstContentBackedEntityTypes = querycontract.GraphFirstContentBackedEntityTypes

// buildLanguageResult converts a Neo4j result row into the response shape.
func buildLanguageResult(row map[string]any, label string) map[string]any {
	result := map[string]any{
		"entity_id": StringVal(row, "entity_id"),
		"name":      StringVal(row, "name"),
	}

	if v := StringSliceVal(row, "labels"); v != nil {
		result["labels"] = v
	}
	if v := StringVal(row, "file_path"); v != "" {
		result["file_path"] = v
	}
	if v := StringVal(row, "repo_id"); v != "" {
		result["repo_id"] = v
	}
	if v := StringVal(row, "repo_name"); v != "" {
		result["repo_name"] = v
	}
	if v := StringVal(row, "language"); v != "" {
		result["language"] = v
	}

	switch label {
	case "Repository":
		result["id"] = StringVal(row, "id")
		result["name"] = StringVal(row, "name")
		result["local_path"] = StringVal(row, "local_path")
		result["remote_url"] = StringVal(row, "remote_url")
		result["file_count"] = IntVal(row, "file_count")
	case "Directory":
		result["file_count"] = IntVal(row, "file_count")
	default:
		if v := IntVal(row, "start_line"); v != 0 {
			result["start_line"] = v
		}
		if v := IntVal(row, "end_line"); v != 0 {
			result["end_line"] = v
		}
		if metadata := graphResultMetadata(row); len(metadata) > 0 {
			result["metadata"] = metadata
			attachSemanticSummary(result)
		}
	}

	return result
}

// graphResultMetadata forwards to querycontract.GraphResultMetadata. The
// implementation moved to querycontract for #6060; this wrapper keeps
// root callers unchanged.
func graphResultMetadata(row map[string]any) map[string]any {
	return querycontract.GraphResultMetadata(row)
}

// graphSemanticMetadataProjection forwards to
// querygraphrows.GraphSemanticMetadataProjection. See that package's
// doc.go for why the fragment lives there rather than in querycontract.
func graphSemanticMetadataProjection() string {
	return querygraphrows.GraphSemanticMetadataProjection()
}

// graphLabelToContentEntityType forwards to
// querycontract.GraphLabelToContentEntityType. The implementation moved to
// querycontract for #6060; this wrapper keeps root callers unchanged.
func graphLabelToContentEntityType(label string) string {
	return querycontract.GraphLabelToContentEntityType(label)
}

func allSupportedEntityTypes() map[string]string {
	merged := make(map[string]string, len(graphBackedEntityTypes)+len(graphFirstContentBackedEntityTypes)+len(contentBackedEntityTypes))
	for key, value := range graphBackedEntityTypes {
		merged[key] = value
	}
	for key, value := range graphFirstContentBackedEntityTypes {
		merged[key] = value
	}
	for key, value := range contentBackedEntityTypes {
		merged[key] = value
	}
	return merged
}

// acceptLanguageQueryEntityType reports whether entityType is one a dispatch
// branch of handleLanguageQuery serves, writing the 400 below and reporting
// false when it is not. It mirrors codequery.ApplyRepositorySelectorForAccess'
// write-and-report-false shape so the handler's request-validation block stays
// one line per check.
func acceptLanguageQueryEntityType(w http.ResponseWriter, entityType string) bool {
	if _, ok := allSupportedEntityTypes()[entityType]; ok {
		return true
	}
	writeLanguageQueryUnsupportedEntityType(w, entityType)
	return false
}

// writeLanguageQueryUnsupportedEntityType writes the route's documented 400 for
// an entity type no dispatch branch of handleLanguageQuery serves.
//
// Two call sites go through it so they can never describe the same rejection
// differently. The first is the request-time gate, which tests membership of
// allSupportedEntityTypes() -- exactly the union of the three dispatch maps --
// with the rest of the request validation. The second is the tail of the
// dispatch chain, which is where the check used to live alone: that tail runs
// AFTER the empty-grant short-circuit, so a scoped caller with no repository
// grants was answered with the route's empty 200 for an entity type the route
// does not support while every other caller got this 400. Two callers
// disagreeing about whether a request is well-formed is a contract bug on its
// own, and the difference also signals the caller's grant state through a
// response the empty page exists to keep uninformative.
//
// The tail call site is unreachable while the gate and the dispatch maps agree.
// It stays as the backstop for the day they drift: an entity type registered in
// allSupportedEntityTypes() but not yet dispatched would otherwise fall out of
// the handler with no response written at all.
func writeLanguageQueryUnsupportedEntityType(w http.ResponseWriter, entityType string) {
	WriteError(w, http.StatusBadRequest, fmt.Sprintf(
		"unsupported entity_type %q; supported: %s",
		entityType, joinKeys(allSupportedEntityTypes()),
	))
}
