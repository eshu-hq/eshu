// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"crypto/sha1" // #nosec G505 -- non-cryptographic content-addressing digest for snapshot deduplication, not a security primitive
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/codeowners"
	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/content/shape"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func resolveNativeSnapshotFileSetForTargets(
	repoPath string,
	fileTargets []string,
	registry parser.Registry,
) (discovery.RepoFileSet, error) {
	files := make([]discovery.FileWithSize, 0, len(fileTargets))
	for _, target := range fileTargets {
		absoluteTarget, err := filepath.Abs(target)
		if err != nil {
			return discovery.RepoFileSet{}, fmt.Errorf("resolve file target %q: %w", target, err)
		}
		if resolvedTarget, resolveErr := filepath.EvalSymlinks(absoluteTarget); resolveErr == nil {
			absoluteTarget = resolvedTarget
		}
		relativePath, err := filepath.Rel(repoPath, absoluteTarget)
		if err != nil {
			return discovery.RepoFileSet{}, fmt.Errorf("relativize file target %q: %w", absoluteTarget, err)
		}
		if relativePath == "." || strings.HasPrefix(relativePath, "..") {
			return discovery.RepoFileSet{}, fmt.Errorf(
				"file target %q is outside repository root %q",
				absoluteTarget,
				repoPath,
			)
		}
		if !isTerraformStateCandidateName(filepath.Base(absoluteTarget)) {
			if _, ok := registry.LookupByPath(absoluteTarget); !ok {
				_, isCodeownersCandidate := codeowners.IsCandidatePath(filepath.ToSlash(filepath.Clean(relativePath)))
				if !isGitDocumentationPath(absoluteTarget) && !isCodeownersCandidate {
					continue
				}
			}
		}
		// Carry the on-disk size so explicit --file-targets partitioning
		// weights each file by its real byte size, byte-identical to the old
		// os.Stat path (max(size, floor)). An unstattable target falls back to
		// the SizeUnavailable sentinel (default weight), matching the old
		// os.Stat-failure behavior.
		fileEntry := discovery.FileWithSize{Path: absoluteTarget, Size: discovery.SizeUnavailable}
		if info, statErr := os.Stat(absoluteTarget); statErr == nil {
			fileEntry.Size = info.Size()
		}
		files = append(files, fileEntry)
	}
	return discovery.RepoFileSet{
		RepoRoot: repoPath,
		Files:    files,
	}, nil
}

func entityBucketsFromParsed(payload map[string]any) map[string][]shape.Entity {
	buckets := make(map[string][]shape.Entity)
	fileArtifactType := snapshotPayloadString(payload, "artifact_type")
	fileTemplateDialect := snapshotPayloadString(payload, "template_dialect")
	fileIACRelevant := snapshotPayloadBoolPtr(payload, "iac_relevant")
	fileLanguage := snapshotPayloadString(payload, "lang", "language")
	for _, mapping := range snapshotEntityBuckets {
		items, _ := payload[mapping.bucket].([]map[string]any)
		if len(items) == 0 {
			continue
		}

		entities := make([]shape.Entity, 0, len(items))
		for _, item := range items {
			artifactType := snapshotPayloadString(item, "artifact_type")
			if artifactType == "" {
				artifactType = fileArtifactType
			}
			templateDialect := snapshotPayloadString(item, "template_dialect")
			if templateDialect == "" {
				templateDialect = fileTemplateDialect
			}
			iacRelevant := snapshotPayloadBoolPtr(item, "iac_relevant")
			if iacRelevant == nil {
				iacRelevant = fileIACRelevant
			}
			language := snapshotPayloadString(item, "lang", "language")
			if language == "" {
				language = fileLanguage
			}
			entities = append(entities, shape.Entity{
				Name:            snapshotPayloadString(item, "name"),
				LineNumber:      snapshotPayloadInt(item, "line_number"),
				EndLine:         snapshotPayloadInt(item, "end_line"),
				StartByte:       snapshotPayloadIntPtr(item, "start_byte"),
				EndByte:         snapshotPayloadIntPtr(item, "end_byte"),
				Language:        language,
				ArtifactType:    artifactType,
				TemplateDialect: templateDialect,
				IACRelevant:     iacRelevant,
				Source:          snapshotPayloadString(item, "source"),
				Metadata:        snapshotEntityMetadata(item),
			})
		}
		buckets[mapping.bucket] = entities
	}
	return buckets
}

// buildEntityUIDLookup builds a read-only map from (path, entity type, entity
// name, start line) → entity ID covering every entity in the slice. Value-flow
// builders that resolve parsed-file entities against the materialized entity
// set share this map so it is built once per snapshot instead of once per
// builder. The returned map must not be mutated by any consumer.
func buildEntityUIDLookup(entities []content.EntityRecord) map[string]string {
	lookup := make(map[string]string, len(entities))
	for _, entity := range entities {
		key := entityLookupKey(entity.Path, entity.EntityType, entity.EntityName, entity.StartLine)
		lookup[key] = entity.EntityID
	}
	return lookup
}

// annotateParsedFilesWithEntityIDs annotates each bucket item in parsedFiles
// with its corresponding entity ID by consulting the shared entityUIDLookup map.
// The map is read-only; the parsedFiles slice and its nested maps are mutated
// in-place.
func annotateParsedFilesWithEntityIDs(
	repoPath string,
	parsedFiles []map[string]any,
	entityUIDLookup map[string]string,
) {
	for _, parsedFile := range parsedFiles {
		absolutePath := snapshotPayloadString(parsedFile, "path")
		relativePath, err := filepath.Rel(repoPath, absolutePath)
		if err != nil {
			continue
		}
		relativePath = filepath.ToSlash(filepath.Clean(relativePath))

		for _, mapping := range snapshotEntityBuckets {
			items, _ := parsedFile[mapping.bucket].([]map[string]any)
			for i := range items {
				name := snapshotPayloadString(items[i], "name")
				lineNumber := snapshotPayloadInt(items[i], "line_number")
				key := entityLookupKey(relativePath, mapping.label, name, lineNumber)
				if entityID, ok := entityUIDLookup[key]; ok {
					items[i]["uid"] = entityID
				}
			}
			parsedFile[mapping.bucket] = items
		}
	}
}

func materializationRecordsToMetas(records []content.Record) []ContentFileMeta {
	metas := make([]ContentFileMeta, 0, len(records))
	for _, record := range records {
		metas = append(metas, ContentFileMeta{
			RelativePath:    record.Path,
			Digest:          record.Digest,
			Language:        record.Metadata["language"],
			ArtifactType:    record.Metadata["artifact_type"],
			TemplateDialect: record.Metadata["template_dialect"],
			IACRelevant:     snapshotMetadataBoolPtr(record.Metadata, "iac_relevant"),
			CommitSHA:       record.Metadata["commit_sha"],
		})
	}
	return metas
}

func materializationEntitiesToSnapshots(
	entities []content.EntityRecord,
	indexedAt time.Time,
) []ContentEntitySnapshot {
	snapshots := make([]ContentEntitySnapshot, 0, len(entities))
	for _, entity := range entities {
		snapshots = append(snapshots, ContentEntitySnapshot{
			EntityID:        entity.EntityID,
			RelativePath:    entity.Path,
			EntityType:      entity.EntityType,
			EntityName:      entity.EntityName,
			StartLine:       entity.StartLine,
			EndLine:         entity.EndLine,
			StartByte:       entity.StartByte,
			EndByte:         entity.EndByte,
			Language:        entity.Language,
			ArtifactType:    entity.ArtifactType,
			TemplateDialect: entity.TemplateDialect,
			IACRelevant:     entity.IACRelevant,
			SourceCache:     entity.SourceCache,
			Metadata:        cloneAnyMap(entity.Metadata),
			IndexedAt:       indexedAt,
		})
	}
	return snapshots
}

func gitCommitSHA(ctx context.Context, repoPath string) string {
	command := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "HEAD") // #nosec G204 -- runs git with fixed internally-constructed arguments, no user input
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// gitCommitSHAFn is the seam the snapshot uses to resolve HEAD when a
// repository carries no sync-resolved SourceCommitSHA. It exists so tests can
// count how many `git rev-parse HEAD` subprocesses the snapshot path runs,
// which is the measured before/after for the #4880 SourceCommitSHA carry
// (1 invocation on the fallback path, 0 when the sync-resolved SHA is carried).
var gitCommitSHAFn = gitCommitSHA

func digestForBody(body string) string {
	sum := sha1.Sum([]byte(body)) // #nosec G401 -- non-cryptographic content-addressing digest for snapshot deduplication, not a security primitive
	return hex.EncodeToString(sum[:])
}

func entityLookupKey(path, entityType, entityName string, lineNumber int) string {
	return strings.Join(
		[]string{
			filepath.ToSlash(strings.TrimSpace(path)),
			strings.TrimSpace(entityType),
			strings.TrimSpace(entityName),
			strconv.Itoa(lineNumber),
		},
		"|",
	)
}

func snapshotPayloadString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if ok {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func snapshotPayloadInt(payload map[string]any, key string) int {
	value, ok := payload[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func snapshotPayloadIntPtr(payload map[string]any, key string) *int {
	value, ok := payload[key]
	if !ok || value == nil {
		return nil
	}
	parsed := snapshotPayloadInt(payload, key)
	return &parsed
}

func snapshotPayloadBoolPtr(payload map[string]any, key string) *bool {
	value, ok := payload[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case bool:
		return &typed
	case string:
		normalized := strings.TrimSpace(strings.ToLower(typed))
		if normalized == "true" {
			value := true
			return &value
		}
		if normalized == "false" {
			value := false
			return &value
		}
	}
	return nil
}

func snapshotMetadataBoolPtr(metadata map[string]string, key string) *bool {
	if metadata == nil {
		return nil
	}
	value, ok := metadata[key]
	if !ok {
		return nil
	}
	normalized := strings.TrimSpace(strings.ToLower(value))
	if normalized == "true" {
		parsed := true
		return &parsed
	}
	if normalized == "false" {
		parsed := false
		return &parsed
	}
	return nil
}
