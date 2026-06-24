// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/blake2s"
)

// Record captures one source-local content write candidate.
type Record struct {
	Path     string
	Body     string
	Digest   string
	Deleted  bool
	Metadata map[string]string

	// PurgeEntities requests removal of any existing content_entities for this
	// path while keeping the content body. Set when per-file entity
	// materialization was skipped for an oversized file so stale symbols are
	// not left queryable.
	PurgeEntities bool
}

// Clone returns a copy-safe record value.
func (r Record) Clone() Record {
	cloned := r
	if r.Metadata != nil {
		cloned.Metadata = cloneStringMap(r.Metadata)
	}

	return cloned
}

// EntityRecord captures one source-local content entity write candidate.
type EntityRecord struct {
	EntityID        string
	Path            string
	EntityType      string
	EntityName      string
	StartLine       int
	EndLine         int
	StartByte       *int
	EndByte         *int
	Language        string
	ArtifactType    string
	TemplateDialect string
	IACRelevant     *bool
	SourceCache     string
	Metadata        map[string]any
	Deleted         bool
}

// RepositoryRef captures one observed source-control reference for a repository.
type RepositoryRef struct {
	Name       string
	Kind       string
	HeadSHA    string
	Default    bool
	ObservedAt time.Time
}

// Clone returns a copy-safe entity record value.
func (r EntityRecord) Clone() EntityRecord {
	cloned := r
	if r.StartByte != nil {
		cloned.StartByte = cloneIntPtr(r.StartByte)
	}
	if r.EndByte != nil {
		cloned.EndByte = cloneIntPtr(r.EndByte)
	}
	if r.IACRelevant != nil {
		cloned.IACRelevant = cloneBoolPtr(r.IACRelevant)
	}
	if r.Metadata != nil {
		cloned.Metadata = cloneAnyMap(r.Metadata)
	}

	return cloned
}

// Materialization is the source-local content payload for one scope generation.
type Materialization struct {
	RepoID         string
	ScopeID        string
	GenerationID   string
	SourceSystem   string
	Records        []Record
	Entities       []EntityRecord
	RepositoryRefs []RepositoryRef

	// FileEntityCapHits counts files where per-file entity materialization was
	// skipped entirely because the projected entity count exceeded
	// shape.MaxFileEntityCount. These are typically minified JS bundles or
	// generated source files.
	FileEntityCapHits int
}

// ScopeGenerationKey returns the durable scope-generation boundary.
func (m Materialization) ScopeGenerationKey() string {
	return fmt.Sprintf("%s:%s", m.ScopeID, m.GenerationID)
}

// Clone returns a copy-safe materialization.
func (m Materialization) Clone() Materialization {
	cloned := m
	if len(m.Records) > 0 {
		cloned.Records = make([]Record, len(m.Records))
		for i := range m.Records {
			cloned.Records[i] = m.Records[i].Clone()
		}
	}
	if len(m.Entities) > 0 {
		cloned.Entities = make([]EntityRecord, len(m.Entities))
		for i := range m.Entities {
			cloned.Entities[i] = m.Entities[i].Clone()
		}
	}
	if len(m.RepositoryRefs) > 0 {
		cloned.RepositoryRefs = append([]RepositoryRef(nil), m.RepositoryRefs...)
	}

	return cloned
}

// Result summarizes one source-local content write.
type Result struct {
	ScopeID            string
	GenerationID       string
	RecordCount        int
	EntityCount        int
	RepositoryRefCount int
	DeletedCount       int
}

// Writer is the narrow source-local content write contract.
type Writer interface {
	Write(context.Context, Materialization) (Result, error)
}

// MemoryWriter is a tiny in-memory writer useful in tests and adapters.
type MemoryWriter struct {
	Writes []Materialization
}

// Write stores a clone of the materialization and returns a derived result.
func (w *MemoryWriter) Write(_ context.Context, materialization Materialization) (Result, error) {
	if w == nil {
		return Result{}, fmt.Errorf("memory writer is nil")
	}

	cloned := materialization.Clone()
	w.Writes = append(w.Writes, cloned)

	result := Result{
		ScopeID:            cloned.ScopeID,
		GenerationID:       cloned.GenerationID,
		RecordCount:        len(cloned.Records),
		EntityCount:        len(cloned.Entities),
		RepositoryRefCount: len(cloned.RepositoryRefs),
	}
	for _, record := range cloned.Records {
		if record.Deleted {
			result.DeletedCount++
		}
	}
	for _, entity := range cloned.Entities {
		if entity.Deleted {
			result.DeletedCount++
		}
	}

	return result, nil
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

// CanonicalEntityID returns a stable content-entity identifier.
func CanonicalEntityID(
	repoID string,
	relativePath string,
	entityType string,
	entityName string,
	lineNumber int,
) string {
	identity := fmt.Sprintf(
		"%s\n%s\n%s\n%s\n%d",
		strings.TrimSpace(repoID),
		strings.TrimSpace(relativePath),
		strings.ToLower(strings.TrimSpace(entityType)),
		strings.TrimSpace(entityName),
		lineNumber,
	)
	sum := blake2s.Sum256([]byte(identity))
	return fmt.Sprintf("content-entity:e_%s", hex.EncodeToString(sum[:])[:12])
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}

	cloned := *value
	return &cloned
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}

	cloned := *value
	return &cloned
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
