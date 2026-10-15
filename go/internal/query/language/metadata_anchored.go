// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/entitysemantics"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/taxonomy"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// contentIDAnchoredLabels are the graph labels the route can enrich whose
// graph node id is, by construction, the content_entities primary key of the
// same entity (#7247).
//
// The projector derives these labels' graph id from (repo, path, type, name,
// start line) with content.CanonicalEntityID whatever id the producer supplied
// (projector/canonical/entity_identity.go, canonicalNamePathLineEntityLabels),
// and every content mint site derives the content id from the same tuple with
// content.CanonicalEntityIDWithMetadata, which differs from it only for
// dependency-identity Variables (content/dependency_identity.go). So a graph
// row's own entity_id names its content row exactly, and the enrichment reads
// those rows by id instead of a second filtered page.
//
// The set is the route-reachable members of the projector's derived-id set,
// minus Variable. Labels outside it keep the page read: Module has its own
// MERGE key, Variable can carry a dependency id, and the SQL, Terraform,
// Component, ImplBlock, TypeAlias and Typedef labels are not in the derived-id
// set. metadata_anchored_identity_test.go pins every member against the
// projector's real entity extraction, so a member whose ids stop agreeing
// fails the build's tests rather than silently mis-attaching metadata.
var contentIDAnchoredLabels = map[string]struct{}{
	"Annotation":           {},
	"Class":                {},
	"Enum":                 {},
	"Function":             {},
	"Macro":                {},
	"Struct":               {},
	"TerragruntDependency": {},
	"TypeAnnotation":       {},
	"Union":                {},
}

// languageEntityMetadataByIDReader aliases
// taxonomy.LanguageEntityMetadataByIDReader. *ContentReader (package query)
// implements it; root's language_alias.go holds the compile-time pin.
type languageEntityMetadataByIDReader = taxonomy.LanguageEntityMetadataByIDReader

// Values of the metadata_path dimension and span attribute.
const (
	metadataPathAnchored = "anchored"
	metadataPathPageRead = "page_read"
)

// Span attributes the enrichment sets on the request span.
const (
	spanAttrMetadataPath         = "eshu.language_query.metadata_path"
	spanAttrMetadataRowsEnriched = "eshu.language_query.metadata_rows_enriched"
	spanAttrMetadataRowsMissing  = "eshu.language_query.metadata_rows_missing"
)

// anchoredMetadataReader returns the id-anchored reader when label is
// content-id-anchored and the configured store implements the lookup.
func (h *Handler) anchoredMetadataReader(label string) (languageEntityMetadataByIDReader, bool) {
	if _, ok := contentIDAnchoredLabels[label]; !ok {
		return nil, false
	}
	reader, ok := h.Content.(languageEntityMetadataByIDReader)
	return reader, ok
}

// enrichLanguageResultsByEntityID merges content metadata into graph rows by
// their own entity ids: one bounded lookup with the caller's repository scope
// bound in the statement, no page read. A content row is merged only into the
// graph row with the same id AND the same repository, so a store that answered
// with another repository's row cannot leak it into this answer. merged has
// the same meaning as on enrichLanguageResultsWithContentMetadata. The rows,
// their order, and every non-metadata field are left as the graph returned
// them.
func (h *Handler) enrichLanguageResultsByEntityID(
	ctx context.Context,
	reader languageEntityMetadataByIDReader,
	results []map[string]any,
	repoID string,
	grant codequery.LanguageQueryGrant,
) ([]map[string]any, bool, error) {
	ids := make([]string, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		id := querycontract.StringVal(result, "entity_id")
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		h.recordMetadataEnrichment(ctx, metadataPathAnchored, 0, len(results))
		return results, false, nil
	}

	rows, err := reader.ListEntityMetadataByIDsForAccess(ctx, taxonomy.LanguageEntityIDLookup{
		EntityIDs:            ids,
		RepoID:               repoID,
		AllowedRepositoryIDs: grant.AllowedRepositoryIDs,
	})
	if err != nil {
		return nil, false, fmt.Errorf("enrich language results with anchored content metadata: %w", err)
	}
	byID := make(map[string]querycontract.EntityContent, len(rows))
	for _, row := range rows {
		byID[row.EntityID] = row
	}

	merged := false
	enriched := 0
	for i := range results {
		row, ok := byID[querycontract.StringVal(results[i], "entity_id")]
		if !ok || len(row.Metadata) == 0 || row.RepoID != languageResultRepositoryID(results[i]) {
			continue
		}
		results[i]["metadata"] = mergeGraphFirstMetadata(results[i]["metadata"], row.Metadata)
		entitysemantics.AttachSemanticSummary(results[i])
		merged = true
		enriched++
	}
	h.recordMetadataEnrichment(ctx, metadataPathAnchored, enriched, len(results)-enriched)
	return results, merged, nil
}

// recordMetadataEnrichment writes how the enrichment served this request onto
// the request span and the eshu_dp_language_query_metadata_rows_total counter.
// enriched rows merged content metadata; missing rows reached the enrichment
// and got none. h.Instruments may be nil.
func (h *Handler) recordMetadataEnrichment(ctx context.Context, path string, enriched, missing int) {
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String(spanAttrMetadataPath, path),
		attribute.Int(spanAttrMetadataRowsEnriched, enriched),
		attribute.Int(spanAttrMetadataRowsMissing, missing),
	)
	if h.Instruments == nil || h.Instruments.LanguageQueryMetadataRows == nil {
		return
	}
	if enriched > 0 {
		h.Instruments.LanguageQueryMetadataRows.Add(ctx, int64(enriched), metric.WithAttributes(
			telemetry.AttrMetadataPath(path), telemetry.AttrOutcome("enriched")))
	}
	if missing > 0 {
		h.Instruments.LanguageQueryMetadataRows.Add(ctx, int64(missing), metric.WithAttributes(
			telemetry.AttrMetadataPath(path), telemetry.AttrOutcome("missing")))
	}
}
