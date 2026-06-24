package searchdocs

import "time"

// SemanticContext is an explicit semantic context input for bounded retrieval.
type SemanticContext struct {
	ID           string
	RepoID       string
	Title        string
	Path         string
	ContextText  string
	ServiceID    string
	WorkloadID   string
	Environment  string
	GraphHandles []GraphHandle
	Labels       []string
	SourceIDs    []string
	UpdatedAt    time.Time
}

// ProjectSemanticContext projects one explicit semantic context label into a search document.
func ProjectSemanticContext(input SemanticContext) (Document, Decision) {
	id := clean(input.ID)
	if id == "" {
		return Document{}, excluded(ReasonMissingStableHandle)
	}
	if containsSensitiveContext(input.ContextText) {
		return Document{}, excluded(ReasonSensitiveContext)
	}

	repoID := clean(input.RepoID)
	doc := baseDocument(
		"semantic_context",
		id,
		repoID,
		SourceKindSemanticContext,
		semanticContextTitle(input.Title),
		clean(input.Path),
		input.UpdatedAt,
		TruthBasisReadModel,
		"semantic_context_read_models",
		semanticContextSourceIDs(id, input.SourceIDs),
	)
	doc.ContextText = boundedContext(input.ContextText)
	doc.GraphHandles = appendNonEmptyHandles(
		doc.GraphHandles,
		GraphHandle{Kind: "semantic_context", ID: id},
		GraphHandle{Kind: "service", ID: clean(input.ServiceID)},
		GraphHandle{Kind: "workload", ID: clean(input.WorkloadID)},
		GraphHandle{Kind: "environment", ID: clean(input.Environment)},
	)
	doc.GraphHandles = appendNonEmptyHandles(doc.GraphHandles, cleanGraphHandles(input.GraphHandles)...)
	if !hasBoundedSemanticContextHandle(doc.GraphHandles) {
		return Document{}, excluded(ReasonMissingStableHandle)
	}
	doc.Labels = appendLabels(doc.Labels, "semantic_context")
	doc.Labels = appendLabels(doc.Labels, input.Labels...)
	doc.Labels = cleanLabels(doc.Labels)
	return doc, included()
}

func semanticContextTitle(title string) string {
	title = clean(title)
	if title == "" {
		return "Semantic context"
	}
	return title
}

func semanticContextSourceIDs(id string, sourceIDs []string) []string {
	cleaned := make([]string, 0, len(sourceIDs)+1)
	for _, sourceID := range sourceIDs {
		sourceID = clean(sourceID)
		if sourceID != "" {
			cleaned = append(cleaned, sourceID)
		}
	}
	if len(cleaned) == 0 {
		cleaned = append(cleaned, id)
	}
	return cleaned
}

func cleanGraphHandles(handles []GraphHandle) []GraphHandle {
	cleaned := make([]GraphHandle, 0, len(handles))
	for _, handle := range handles {
		cleaned = append(cleaned, GraphHandle{
			Kind: clean(handle.Kind),
			ID:   clean(handle.ID),
		})
	}
	return cleaned
}

func hasBoundedSemanticContextHandle(handles []GraphHandle) bool {
	for _, handle := range handles {
		switch handle.Kind {
		case "repository", "service", "workload", "environment":
			if handle.ID != "" {
				return true
			}
		default:
			if handle.Kind != "" && handle.Kind != "semantic_context" && handle.ID != "" {
				return true
			}
		}
	}
	return false
}
