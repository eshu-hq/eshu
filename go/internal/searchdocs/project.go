package searchdocs

import (
	"sort"
	"strings"
	"time"
)

const maxContextBytes = 4096

// SourceKind identifies the source lane that produced a search document.
type SourceKind string

const (
	// SourceKindCodeEntity is a document projected from one indexed content entity.
	SourceKindCodeEntity SourceKind = "code_entity"
	// SourceKindRepositoryFile is a document projected from one indexed repository file.
	SourceKindRepositoryFile SourceKind = "repository_file"
	// SourceKindRuntimeSummary is a document projected from runtime/deployment read models.
	SourceKindRuntimeSummary SourceKind = "runtime_summary"
	// SourceKindSemanticContext is a document projected from an explicit semantic context label.
	SourceKindSemanticContext SourceKind = "semantic_context"
)

// TruthLevel is the document-level authority for the projected record.
type TruthLevel string

const (
	// TruthLevelDerived means the document is derived from indexed state, not canonical graph truth.
	TruthLevelDerived TruthLevel = "derived"
)

// TruthBasis names the evidence family behind a projected document.
type TruthBasis string

const (
	// TruthBasisContentIndex means content_files or content_entities supplied the document.
	TruthBasisContentIndex TruthBasis = "content_index"
	// TruthBasisReadModel means an Eshu read model supplied the document.
	TruthBasisReadModel TruthBasis = "read_model"
)

// FreshnessState is the search-document freshness state.
type FreshnessState string

const (
	// FreshnessFresh means the source record is current for the projection input.
	FreshnessFresh FreshnessState = "fresh"
)

// ExclusionReason explains why a projection candidate did not become a document.
type ExclusionReason string

const (
	// ReasonIncluded means the candidate became a search document.
	ReasonIncluded ExclusionReason = "included"
	// ReasonMissingStableHandle means the candidate lacks a durable document or graph handle.
	ReasonMissingStableHandle ExclusionReason = "missing_stable_handle"
	// ReasonSensitiveContext means the candidate text appears to contain secret material.
	ReasonSensitiveContext ExclusionReason = "sensitive_context"
	// ReasonExcludedSourceKind means the candidate source lane is intentionally not searchable.
	ReasonExcludedSourceKind ExclusionReason = "excluded_source_kind"
)

// Document is one curated search-lane record.
type Document struct {
	ID           string
	RepoID       string
	SourceKind   SourceKind
	Title        string
	Path         string
	ContextText  string
	EntityRefs   []EntityRef
	GraphHandles []GraphHandle
	Labels       []string
	UpdatedAt    time.Time
	TruthScope   TruthScope
	Freshness    Freshness
	AccessScope  AccessScope
	Provenance   Provenance
}

// EntityRef points from a document back to a content entity.
type EntityRef struct {
	ID        string
	Type      string
	Name      string
	Path      string
	StartLine int
	EndLine   int
}

// GraphHandle is a bounded graph-expansion candidate.
type GraphHandle struct {
	Kind string
	ID   string
}

// TruthScope describes why the document is not canonical truth.
type TruthScope struct {
	Level TruthLevel
	Basis TruthBasis
}

// Freshness captures freshness state for the projected document.
type Freshness struct {
	State FreshnessState
}

// AccessScope keeps later retrieval authorization anchored to a small scope.
type AccessScope struct {
	RepoID string
}

// Provenance records the source rows used to build the document.
type Provenance struct {
	SourceTable string
	SourceIDs   []string
}

// Decision records whether a projection candidate was included.
type Decision struct {
	Include bool
	Reason  ExclusionReason
}

// ContentEntity is the content_entities input used by the first search projection.
type ContentEntity struct {
	EntityID     string
	RepoID       string
	RelativePath string
	EntityType   string
	EntityName   string
	StartLine    int
	EndLine      int
	Language     string
	ArtifactType string
	SourceCache  string
	Metadata     map[string]string
	IndexedAt    time.Time
}

// ContentFile is the content_files input used by the first search projection.
type ContentFile struct {
	RepoID       string
	RelativePath string
	Language     string
	ArtifactType string
	Content      string
	IndexedAt    time.Time
}

// RuntimeSummary is a bounded runtime/deployment summary input.
type RuntimeSummary struct {
	ID          string
	RepoID      string
	Title       string
	Summary     string
	ServiceID   string
	WorkloadID  string
	ImageDigest string
	UpdatedAt   time.Time
}

// ProjectContentEntity projects one indexed entity into a curated search document.
func ProjectContentEntity(input ContentEntity) (Document, Decision) {
	entityID := clean(input.EntityID)
	repoID := clean(input.RepoID)
	if entityID == "" || repoID == "" {
		return Document{}, excluded(ReasonMissingStableHandle)
	}
	if excludedEntityType(input.EntityType) || excludedArtifactType(input.ArtifactType) || metadataMarksExcluded(input.Metadata) {
		return Document{}, excluded(ReasonExcludedSourceKind)
	}
	if containsSensitiveContext(input.SourceCache) {
		return Document{}, excluded(ReasonSensitiveContext)
	}

	entityType := clean(input.EntityType)
	entityName := clean(input.EntityName)
	relativePath := clean(input.RelativePath)
	doc := baseDocument(
		"content_entity",
		entityID,
		repoID,
		SourceKindCodeEntity,
		entityTitle(entityType, entityName),
		relativePath,
		input.IndexedAt,
		TruthBasisContentIndex,
		"content_entities",
		[]string{entityID},
	)
	doc.ContextText = boundedContext(input.SourceCache)
	doc.EntityRefs = []EntityRef{{
		ID:        entityID,
		Type:      entityType,
		Name:      entityName,
		Path:      relativePath,
		StartLine: input.StartLine,
		EndLine:   input.EndLine,
	}}
	doc.GraphHandles = appendNonEmptyHandles(
		doc.GraphHandles,
		GraphHandle{Kind: "content_entity", ID: entityID},
		fileHandle(repoID, relativePath),
	)
	doc.Labels = appendLabels(
		doc.Labels,
		label("language", input.Language),
		label("entity_type", entityType),
		label("artifact_type", input.ArtifactType),
	)
	doc.Labels = cleanLabels(doc.Labels)
	return doc, included()
}

// ProjectContentFile projects one indexed file into a curated search document.
func ProjectContentFile(input ContentFile) (Document, Decision) {
	repoID := clean(input.RepoID)
	relativePath := clean(input.RelativePath)
	if repoID == "" || relativePath == "" {
		return Document{}, excluded(ReasonMissingStableHandle)
	}
	if excludedArtifactType(input.ArtifactType) {
		return Document{}, excluded(ReasonExcludedSourceKind)
	}
	if containsSensitiveContext(input.Content) {
		return Document{}, excluded(ReasonSensitiveContext)
	}

	doc := baseDocument(
		"content_file",
		repoID+":"+relativePath,
		repoID,
		SourceKindRepositoryFile,
		"File "+relativePath,
		relativePath,
		input.IndexedAt,
		TruthBasisContentIndex,
		"content_files",
		[]string{repoID + ":" + relativePath},
	)
	doc.ContextText = boundedContext(input.Content)
	doc.GraphHandles = append(doc.GraphHandles, GraphHandle{Kind: "file", ID: repoID + ":" + relativePath})
	doc.Labels = appendLabels(
		doc.Labels,
		label("language", input.Language),
		label("artifact_type", input.ArtifactType),
	)
	doc.Labels = cleanLabels(doc.Labels)
	return doc, included()
}

// ProjectRuntimeSummary projects one bounded runtime summary into a search document.
func ProjectRuntimeSummary(input RuntimeSummary) (Document, Decision) {
	id := clean(input.ID)
	if id == "" {
		return Document{}, excluded(ReasonMissingStableHandle)
	}
	if containsSensitiveContext(input.Summary) {
		return Document{}, excluded(ReasonSensitiveContext)
	}

	doc := baseDocument(
		"runtime_summary",
		id,
		clean(input.RepoID),
		SourceKindRuntimeSummary,
		clean(input.Title),
		"",
		input.UpdatedAt,
		TruthBasisReadModel,
		"runtime_read_models",
		[]string{id},
	)
	doc.ContextText = boundedContext(input.Summary)
	doc.GraphHandles = appendNonEmptyHandles(
		doc.GraphHandles,
		GraphHandle{Kind: "runtime_summary", ID: id},
		GraphHandle{Kind: "service", ID: clean(input.ServiceID)},
		GraphHandle{Kind: "workload", ID: clean(input.WorkloadID)},
		GraphHandle{Kind: "container_image", ID: clean(input.ImageDigest)},
	)
	doc.Labels = appendLabels(doc.Labels, "runtime")
	doc.Labels = cleanLabels(doc.Labels)
	return doc, included()
}

func baseDocument(
	sourcePrefix string,
	sourceID string,
	repoID string,
	sourceKind SourceKind,
	title string,
	path string,
	updatedAt time.Time,
	basis TruthBasis,
	sourceTable string,
	sourceIDs []string,
) Document {
	doc := Document{
		ID:          "searchdoc:" + sourcePrefix + ":" + sourceID,
		RepoID:      repoID,
		SourceKind:  sourceKind,
		Title:       title,
		Path:        path,
		UpdatedAt:   updatedAt,
		TruthScope:  TruthScope{Level: TruthLevelDerived, Basis: basis},
		Freshness:   Freshness{State: FreshnessFresh},
		AccessScope: AccessScope{RepoID: repoID},
		Provenance:  Provenance{SourceTable: sourceTable, SourceIDs: sourceIDs},
	}
	doc.GraphHandles = appendNonEmptyHandles(doc.GraphHandles, GraphHandle{Kind: "repository", ID: repoID})
	return doc
}

func entityTitle(entityType string, entityName string) string {
	switch {
	case entityType != "" && entityName != "":
		return entityType + " " + entityName
	case entityName != "":
		return entityName
	case entityType != "":
		return entityType
	default:
		return "Content entity"
	}
}

func boundedContext(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxContextBytes {
		return value
	}
	return value[:maxContextBytes]
}

func containsSensitiveContext(value string) bool {
	lower := strings.ToLower(value)
	sensitiveTerms := []string{
		"api_key",
		"apikey",
		"access_key",
		"password",
		"passwd",
		"private_key",
		"secret",
		"token",
	}
	for _, term := range sensitiveTerms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func excludedEntityType(entityType string) bool {
	switch normalizedKind(entityType) {
	case "dashboardasset", "queryexecution":
		return true
	default:
		return false
	}
}

func excludedArtifactType(artifactType string) bool {
	switch normalizedKind(artifactType) {
	case "dashboardasset", "dashboardjson", "findingbody", "logline", "providerpayload", "querybody", "rawproviderpayload", "securityfindingbody", "tracespan":
		return true
	default:
		return false
	}
}

func metadataMarksExcluded(metadata map[string]string) bool {
	for key := range metadata {
		switch normalizedKind(key) {
		case "findingbody", "logline", "querybody", "rawpayload", "tracespan":
			return true
		}
	}
	return false
}

func normalizedKind(value string) string {
	value = strings.ToLower(clean(value))
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, ".", "")
	return value
}

func appendNonEmptyHandles(handles []GraphHandle, candidates ...GraphHandle) []GraphHandle {
	for _, candidate := range candidates {
		if candidate.Kind == "" || candidate.ID == "" {
			continue
		}
		handles = append(handles, candidate)
	}
	return handles
}

func appendLabels(labels []string, candidates ...string) []string {
	for _, candidate := range candidates {
		candidate = clean(candidate)
		if candidate == "" {
			continue
		}
		labels = append(labels, candidate)
	}
	return labels
}

func cleanLabels(labels []string) []string {
	seen := make(map[string]struct{}, len(labels))
	cleaned := make([]string, 0, len(labels))
	for _, value := range labels {
		value = clean(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	sort.Strings(cleaned)
	return cleaned
}

func label(prefix string, value string) string {
	value = strings.ToLower(clean(value))
	if value == "" {
		return ""
	}
	return prefix + ":" + value
}

func fileHandle(repoID string, relativePath string) GraphHandle {
	if repoID == "" || relativePath == "" {
		return GraphHandle{}
	}
	return GraphHandle{Kind: "file", ID: repoID + ":" + relativePath}
}

func clean(value string) string {
	return strings.TrimSpace(value)
}

func included() Decision {
	return Decision{Include: true, Reason: ReasonIncluded}
}

func excluded(reason ExclusionReason) Decision {
	return Decision{Reason: reason}
}
