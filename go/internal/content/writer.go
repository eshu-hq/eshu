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

// dependencyIdentityPackageManagers is the set of metadata["package_manager"]
// values whose manifest (non-lockfile) dependency Variables qualify for the
// section-keyed CanonicalDependencyEntityID scheme. See
// CanonicalEntityIDWithMetadata for the full gate and why it is restricted to
// exactly these two producers.
var dependencyIdentityPackageManagers = map[string]struct{}{
	"npm":      {},
	"composer": {},
}

// CanonicalEntityIDWithMetadata returns the canonical content-entity
// identifier for entityType/entityName at lineNumber, routing in-scope
// manifest dependency Variables to the section-keyed, line-independent
// CanonicalDependencyEntityID and everything else to the legacy line-keyed
// CanonicalEntityID.
//
// An entity qualifies for the dependency form IFF ALL of:
//
//  1. entityType is "variable" (case-insensitive, matching CanonicalEntityID's
//     own normalization);
//  2. metadata["config_kind"] == "dependency";
//  3. metadata["package_manager"] is "npm" or "composer";
//  4. metadata["lockfile"] is not true. Checked defensively as either a native
//     bool (collector snapshot site) or a JSON-decoded bool/"true" string
//     (projector fact-replay site) — anything else (absent, false, other
//     string) passes this condition;
//  5. metadata["section"], trimmed, is a non-empty string.
//
// This narrow gate exists because metadata["config_kind"] == "dependency"
// alone is also emitted by lockfile parsers (package-lock.json,
// composer.lock, and other npm lockfile flavors), which legitimately repeat a
// package name multiple times within one section — nested node_modules can
// carry the same name at different versions. Collapsing those rows under
// (path, section, name) would silently merge distinct dependency versions
// into one identity, an accuracy violation. Conditions 3 and 4 restrict the
// new identity to exactly the two emitters that guarantee per-section name
// uniqueness by construction: package.json's dependencyVariablesWithScope
// (parser/json/language.go) walking the dependencies/devDependencies/
// optionalDependencies/peerDependencies sections, and composer.json's
// composerManifestDependencyVariables walking require/require-dev. Every
// lockfile producer sets metadata["lockfile"] = true; every other manifest
// format (maven, gradle, pythondep, cargo, ...) uses a different
// package_manager value and is intentionally out of scope until its own
// per-section uniqueness is proven — do not extend
// dependencyIdentityPackageManagers without that proof.
//
// Anything that does not satisfy all five conditions returns
// CanonicalEntityID unchanged, so code Variables, Functions, tsconfig rows,
// lockfile rows, and out-of-scope manifest formats keep their existing
// line-keyed identity.
func CanonicalEntityIDWithMetadata(
	repoID string,
	relativePath string,
	entityType string,
	entityName string,
	lineNumber int,
	metadata map[string]any,
) string {
	if section, ok := dependencyIdentitySection(entityType, metadata); ok {
		return CanonicalDependencyEntityID(repoID, relativePath, section, entityName)
	}
	return CanonicalEntityID(repoID, relativePath, entityType, entityName, lineNumber)
}

// dependencyIdentitySection applies the five-condition gate documented on
// CanonicalEntityIDWithMetadata and returns the trimmed section name when the
// entity qualifies for section-keyed dependency identity.
func dependencyIdentitySection(entityType string, metadata map[string]any) (string, bool) {
	if !strings.EqualFold(strings.TrimSpace(entityType), "variable") {
		return "", false
	}
	if metadataStringValue(metadata, "config_kind") != "dependency" {
		return "", false
	}
	if _, ok := dependencyIdentityPackageManagers[metadataStringValue(metadata, "package_manager")]; !ok {
		return "", false
	}
	if metadataIsTrue(metadata, "lockfile") {
		return "", false
	}
	section := strings.TrimSpace(metadataStringValue(metadata, "section"))
	if section == "" {
		return "", false
	}
	return section, true
}

// metadataStringValue reads a string-typed metadata value, returning "" for a
// missing key or a value of any other type.
func metadataStringValue(metadata map[string]any, key string) string {
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return text
}

// metadataIsTrue reports whether metadata[key] represents boolean true,
// accepting both a native bool (set by the collector snapshot mint site) and
// a JSON-decoded bool or "true"/"TRUE" string (set by the projector
// fact-replay fallback, which may see either shape depending on transport).
func metadataIsTrue(metadata map[string]any, key string) bool {
	switch typed := metadata[key].(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

// CanonicalDependencyEntityID returns the section-keyed, line-independent
// content-entity identifier for an in-scope manifest dependency Variable (see
// CanonicalEntityIDWithMetadata's gate). Reordering dependencies within a
// manifest section, or a source line shifting because of an unrelated edit
// elsewhere in the file, does not change this identity — unlike the
// line-keyed CanonicalEntityID.
//
// The hash input is domain-tagged ("eshu-dep-v1") and six newline-joined
// components wide: the tag, repoID, relativePath, the constant "variable",
// section, and name. CanonicalEntityID's input is five components with no
// tag, so a dependency Variable's identity can never collide with a code
// Variable's identity for the same (repo, path, name) — the tag plus the
// differing component count give unconditional domain separation.
func CanonicalDependencyEntityID(repoID, relativePath, section, name string) string {
	identity := fmt.Sprintf(
		"eshu-dep-v1\n%s\n%s\n%s\n%s\n%s",
		strings.TrimSpace(repoID),
		strings.TrimSpace(relativePath),
		"variable",
		strings.TrimSpace(section),
		strings.TrimSpace(name),
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
