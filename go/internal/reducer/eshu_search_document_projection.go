package reducer

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// SearchDocumentProjectionInput is the bounded source set projected into curated
// search documents for one scope and generation. Each lane is curated
// independently through searchdocs so excluded or sensitive source rows never
// become search documents.
type SearchDocumentProjectionInput struct {
	ContentEntities  []searchdocs.ContentEntity
	ContentFiles     []searchdocs.ContentFile
	RuntimeSummaries []searchdocs.RuntimeSummary
}

// SearchDocumentCurationSummary records bounded, operator-facing counts for one
// projection cycle. Counts are low cardinality (source kind, exclusion reason)
// so they are safe as telemetry attributes.
type SearchDocumentCurationSummary struct {
	// Considered is the total number of source rows examined across all lanes.
	Considered int
	// Included is the number of curated documents produced.
	Included int
	// IncludedBySourceKind counts produced documents by curated source kind.
	IncludedBySourceKind map[searchdocs.SourceKind]int
	// SkippedByReason counts dropped candidates by searchdocs exclusion reason.
	SkippedByReason map[searchdocs.ExclusionReason]int
}

// SearchDocumentProjection is the curated document set plus its curation summary.
type SearchDocumentProjection struct {
	Documents []searchdocs.Document
	Summary   SearchDocumentCurationSummary
}

// newSearchDocumentCurationSummary returns an empty summary ready to aggregate
// per-page projection summaries across a streamed scope.
func newSearchDocumentCurationSummary() SearchDocumentCurationSummary {
	return SearchDocumentCurationSummary{
		IncludedBySourceKind: make(map[searchdocs.SourceKind]int),
		SkippedByReason:      make(map[searchdocs.ExclusionReason]int),
	}
}

// merge folds one page's curation summary into the running total so the handler
// emits accurate aggregate counts across all streamed pages.
func (s *SearchDocumentCurationSummary) merge(page SearchDocumentCurationSummary) {
	s.Considered += page.Considered
	s.Included += page.Included
	for kind, count := range page.IncludedBySourceKind {
		s.IncludedBySourceKind[kind] += count
	}
	for reason, count := range page.SkippedByReason {
		s.SkippedByReason[reason] += count
	}
}

// ProjectSearchDocuments curates the bounded input into derived search documents.
// Excluded or sensitive candidates are dropped and counted by reason, and the
// returned documents are ordered by ID so generation-scoped writes are
// idempotent under retries.
func ProjectSearchDocuments(input SearchDocumentProjectionInput) SearchDocumentProjection {
	summary := SearchDocumentCurationSummary{
		IncludedBySourceKind: make(map[searchdocs.SourceKind]int),
		SkippedByReason:      make(map[searchdocs.ExclusionReason]int),
	}
	total := len(input.ContentEntities) + len(input.ContentFiles) + len(input.RuntimeSummaries)
	documents := make([]searchdocs.Document, 0, total)

	record := func(doc searchdocs.Document, decision searchdocs.Decision) {
		summary.Considered++
		if decision.Include {
			documents = append(documents, doc)
			summary.Included++
			summary.IncludedBySourceKind[doc.SourceKind]++
			return
		}
		summary.SkippedByReason[decision.Reason]++
	}

	for _, entity := range input.ContentEntities {
		record(searchdocs.ProjectContentEntity(entity))
	}
	for _, file := range input.ContentFiles {
		record(searchdocs.ProjectContentFile(file))
	}
	for _, runtimeSummary := range input.RuntimeSummaries {
		record(searchdocs.ProjectRuntimeSummary(runtimeSummary))
	}

	sort.Slice(documents, func(i int, j int) bool {
		return documents[i].ID < documents[j].ID
	})
	return SearchDocumentProjection{Documents: documents, Summary: summary}
}
