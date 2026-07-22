// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shape

import (
	"fmt"
	pathpkg "path"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/content"
)

const (
	githubActionsWorkflowArtifactType = "github_actions_workflow"

	// MaxFileEntityCount is the maximum number of content entities emitted for a
	// single file. Files whose parser output would exceed this limit have entity
	// materialization skipped entirely: the content record (body, digest) is still
	// written so full-file BM25 search works, but no per-entity rows are produced.
	//
	// The threshold is set to catch minified JavaScript (e.g. ckeditor.js → 24k
	// entities) and pathological generated files (e.g. a single PHP class file →
	// 53k entities) that inflate ingestion time and search-index volume without
	// contributing useful symbol-level evidence. Real source files at repo scale
	// stay well below 10,000 entities; this limit gives comfortable headroom.
	//
	// Observed on the full-corpus run (issue #3676):
	//   ckeditor/ckeditor.js  → 24,720 entities (minified JS)
	//   yacht.class.php       → 53,830 entities (generated PHP)
	MaxFileEntityCount = 10_000
)

// Input captures one normalized parser payload batch for content shaping.
type Input struct {
	RepoID       string
	SourceSystem string
	Files        []File
}

// File captures one parser-shaped file payload and its nested entity buckets.
type File struct {
	Path            string
	Body            string
	Digest          string
	Language        string
	ArtifactType    string
	TemplateDialect string
	IACRelevant     *bool
	CommitSHA       string
	Metadata        map[string]string
	Deleted         bool
	EntityBuckets   map[string][]Entity

	// ParseBounded is true when the parser skipped its tree-sitter parse for
	// this file entirely because it exceeded a parser-level byte cap (the
	// #4766 JS/TS/TSX/PHP 1 MiB cap: payload["js_parse_bounded"] /
	// payload["php_parse_bounded"]). EntityBuckets is empty in that case not
	// because the file has no symbols, but because the parse never ran. This
	// mirrors the per-file entity-count cap (fileEntityCapHit below): both
	// mean "no new entities were produced for a reason unrelated to the
	// file's actual content" and both must force PurgeEntities so the writer
	// retracts any stale content_entities left from a prior indexing run.
	ParseBounded bool
}

// Entity captures one parser-shaped entity payload.
type Entity struct {
	Name            string
	LineNumber      int
	EndLine         int
	StartByte       *int
	EndByte         *int
	Language        string
	ArtifactType    string
	TemplateDialect string
	IACRelevant     *bool
	Source          string
	Metadata        map[string]any
	Deleted         bool
}

// Materialize converts parser-shaped payloads into canonical content rows.
func Materialize(input Input) (content.Materialization, error) {
	repoID := strings.TrimSpace(input.RepoID)
	if repoID == "" {
		return content.Materialization{}, fmt.Errorf("repo_id is required")
	}

	materialization := content.Materialization{
		RepoID:       repoID,
		SourceSystem: strings.TrimSpace(input.SourceSystem),
	}

	for _, file := range input.Files {
		record, entities, fileEntityCapHit, err := materializeFile(repoID, file)
		if err != nil {
			return content.Materialization{}, err
		}
		materialization.Records = append(materialization.Records, record)
		materialization.Entities = append(materialization.Entities, entities...)
		if fileEntityCapHit {
			materialization.FileEntityCapHits++
		}
	}

	return materialization, nil
}

// materializeFile returns the content record plus entities for one parsed file,
// along with fileEntityCapHit which is true when the per-file entity count cap
// was applied and entity materialization was skipped entirely. PurgeEntities is
// set on the record so the writer retracts any stale content_entities rows left
// from a prior indexing run whenever the file's entity buckets are empty for a
// reason unrelated to its actual content: the per-file entity-count cap fired
// (fileEntityCapHit), or the parser itself skipped the parse via its byte cap
// (file.ParseBounded, see #4766).
func materializeFile(repoID string, file File) (content.Record, []content.EntityRecord, bool, error) {
	path := strings.TrimSpace(file.Path)
	if path == "" {
		return content.Record{}, nil, false, fmt.Errorf("content file path is required")
	}

	record := content.Record{
		Path:     path,
		Body:     file.Body,
		Digest:   strings.TrimSpace(file.Digest),
		Deleted:  file.Deleted,
		Metadata: normalizeFileMetadata(file),
	}

	entities, fileEntityCapHit, err := materializeEntities(repoID, path, file)
	if err != nil {
		return content.Record{}, nil, false, err
	}
	if fileEntityCapHit || file.ParseBounded {
		record.PurgeEntities = true
	}

	return record, entities, fileEntityCapHit, nil
}

func normalizeFileMetadata(file File) map[string]string {
	metadata := cloneStringMap(file.Metadata)
	if metadata == nil {
		metadata = make(map[string]string)
	}
	setString(metadata, "language", file.Language)
	setString(metadata, "artifact_type", file.ArtifactType)
	setString(metadata, "template_dialect", file.TemplateDialect)
	if file.IACRelevant != nil {
		setString(metadata, "iac_relevant", strings.ToLower(fmt.Sprintf("%t", *file.IACRelevant)))
	}
	setString(metadata, "commit_sha", file.CommitSHA)

	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

// materializeEntities returns entities and a fileEntityCapHit flag.
// fileEntityCapHit is true when the per-file entity cap fired and entity
// materialization was skipped entirely (returned slice is nil). Transitive
// lockfile variable entries are always preserved; the lockfile variable cap
// was removed because transitive entries feed the reducer's
// PackageConsumptionDecision for supply-chain impact and security alerts.
func materializeEntities(repoID string, path string, file File) ([]content.EntityRecord, bool, error) {
	indexedItems := make([]indexedEntity, 0)
	if workflow, ok := githubActionsWorkflowFileEntity(file); ok {
		indexedItems = append(indexedItems, workflow)
	}
	for _, bucket := range contentEntityBuckets {
		items := file.EntityBuckets[bucket.bucket]
		for _, item := range items {
			label := entityLabelForBucket(bucket.label, item)
			indexedItems = append(indexedItems, indexedEntity{
				label: label,
				item:  item,
			})
		}
	}

	// Skip entity materialization for files that would produce an unreasonably
	// large number of entities. This catches minified JS and generated source
	// files that contribute noise to BM25 indexing without symbol-level value.
	// The content record (body, digest) is still written by the caller.
	if len(indexedItems) > MaxFileEntityCount {
		return nil, true, nil
	}

	sort.SliceStable(indexedItems, func(i, j int) bool {
		left := indexedItems[i]
		right := indexedItems[j]
		if left.lineNumber() != right.lineNumber() {
			return left.lineNumber() < right.lineNumber()
		}
		if left.label != right.label {
			return left.label < right.label
		}
		return left.item.Name < right.item.Name
	})

	entities := make([]content.EntityRecord, 0, len(indexedItems))
	for index, indexed := range indexedItems {
		startLine := indexed.lineNumber()
		endLine := entityEndLine(indexedItems, index, file.Body, startLine)
		sourceCache := entitySourceCache(indexed.label, indexed.item, file.Body, startLine, endLine)
		metadata := cloneAnyMap(indexed.item.Metadata)
		sourceCache, metadata = limitEntitySourceCache(indexed.label, sourceCache, metadata)
		entities = append(entities, content.EntityRecord{
			EntityID:        content.CanonicalEntityIDWithMetadata(repoID, path, indexed.label, indexed.item.Name, startLine, metadata),
			Path:            path,
			EntityType:      indexed.label,
			EntityName:      indexed.item.Name,
			StartLine:       startLine,
			EndLine:         endLine,
			StartByte:       indexed.item.StartByte,
			EndByte:         indexed.item.EndByte,
			Language:        firstNonEmpty(indexed.item.Language, file.Language),
			ArtifactType:    firstNonEmpty(indexed.item.ArtifactType, file.ArtifactType),
			TemplateDialect: firstNonEmpty(indexed.item.TemplateDialect, file.TemplateDialect),
			IACRelevant:     cloneBoolPtr(firstBool(indexed.item.IACRelevant, file.IACRelevant)),
			SourceCache:     sourceCache,
			Metadata:        metadata,
			Deleted:         indexed.item.Deleted,
		})
	}

	return entities, false, nil
}

// githubActionsWorkflowFileEntity creates the content-only File entity that
// lets the entity-context fallback inspect GitHub Actions workflow source. It
// deliberately has no parser or graph counterpart.
func githubActionsWorkflowFileEntity(file File) (indexedEntity, bool) {
	if file.Deleted || strings.TrimSpace(file.ArtifactType) != githubActionsWorkflowArtifactType {
		return indexedEntity{}, false
	}

	filename := pathpkg.Base(strings.TrimSpace(file.Path))
	name := strings.TrimSuffix(filename, pathpkg.Ext(filename))
	if name == "" || name == "." {
		return indexedEntity{}, false
	}

	return indexedEntity{
		label: "File",
		item: Entity{
			Name:            name,
			LineNumber:      1,
			EndLine:         lineCount(file.Body),
			Language:        file.Language,
			ArtifactType:    file.ArtifactType,
			TemplateDialect: file.TemplateDialect,
			IACRelevant:     cloneBoolPtr(file.IACRelevant),
			Source:          file.Body,
		},
	}, true
}

func entityEndLine(items []indexedEntity, index int, body string, startLine int) int {
	item := items[index].item
	if item.EndLine >= startLine {
		return item.EndLine
	}
	if nextLine := nextLineNumber(items, index, startLine); nextLine != nil {
		if candidate := *nextLine - 1; candidate >= startLine {
			return candidate
		}
	}
	if totalLines := lineCount(body); totalLines > 0 {
		candidate := startLine + 24
		if totalLines < candidate {
			candidate = totalLines
		}
		if candidate < startLine {
			return startLine
		}
		return candidate
	}
	return startLine
}

func nextLineNumber(items []indexedEntity, index int, startLine int) *int {
	for _, candidate := range items[index+1:] {
		line := candidate.lineNumber()
		if line >= startLine {
			cloned := line
			return &cloned
		}
	}
	return nil
}

func splitLines(body string) []string {
	if body == "" {
		return nil
	}

	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if strings.HasSuffix(normalized, "\n") && len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func lineCount(body string) int {
	if body == "" {
		return 0
	}
	return len(splitLines(body))
}

func withTrailingNewline(contentText string, label string) string {
	if _, ok := trailingNewlineLabels[label]; !ok {
		return contentText
	}
	if contentText == "" || strings.HasSuffix(contentText, "\n") {
		return contentText
	}
	return contentText + "\n"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstBool(values ...*bool) *bool {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func cloneAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}

	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = cloneAnyValue(value)
	}
	return cloned
}

func cloneAnySlice(input []any) []any {
	if input == nil {
		return nil
	}

	cloned := make([]any, len(input))
	for i, value := range input {
		cloned[i] = cloneAnyValue(value)
	}
	return cloned
}

func cloneStringSlice(input []string) []string {
	if input == nil {
		return nil
	}
	return append([]string(nil), input...)
}

func cloneAnyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		return cloneAnySlice(typed)
	case []string:
		return cloneStringSlice(typed)
	default:
		return typed
	}
}

func setString(target map[string]string, key string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	target[key] = value
}

func isCodeSourceLabel(label string) bool {
	_, ok := sourceFieldContainsCode[label]
	return ok
}
