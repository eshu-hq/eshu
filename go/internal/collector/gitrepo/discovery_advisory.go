// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitrepo

import (
	"path/filepath"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
	"github.com/eshu-hq/eshu/go/internal/collector/gitrepo/gitmodel"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const discoveryAdvisorySchemaVersion = "discovery_advisory.v1"

func buildDiscoveryAdvisoryReport(
	repoPath string,
	generatedAt time.Time,
	stats discovery.DiscoveryStats,
	discoveredFiles []string,
	contentFiles []gitmodel.ContentFileMeta,
	entities []gitmodel.ContentEntitySnapshot,
	commitSHA string,
) *collector.DiscoveryAdvisoryReport {
	contentFileCount := len(contentFiles)
	report := &collector.DiscoveryAdvisoryReport{
		SchemaVersion: discoveryAdvisorySchemaVersion,
		GeneratedAt:   generatedAt,
		Run: collector.DiscoveryAdvisoryRun{
			RepoPath:  repoPath,
			CommitSHA: commitSHA,
		},
		Summary: collector.DiscoveryAdvisorySummary{
			DiscoveredFiles:   len(discoveredFiles),
			ParsedFiles:       contentFileCount,
			ParseSkippedFiles: maxInt(len(discoveredFiles)-contentFileCount, 0),
			ContentFiles:      contentFileCount,
			ContentEntities:   len(entities),
			SkippedDirs:       stats.TotalDirsSkipped(),
			SkippedFiles:      stats.TotalFilesSkipped(),
		},
		EntityCounts: collector.DiscoveryAdvisoryEntityCount{
			ByType:           map[string]int{},
			ByLanguage:       map[string]int{},
			BySourceFileKind: map[string]int{},
		},
		SkipBreakdown: collector.DiscoveryAdvisorySkipBreakdown{
			DirsByName:       cloneIntMap(stats.DirsSkippedByName),
			DirsByUser:       cloneIntMap(stats.DirsSkippedByUser),
			FilesByExtension: cloneIntMap(stats.FilesSkippedByExtension),
			FilesByContent:   cloneIntMap(stats.FilesSkippedByContent),
			FilesByUser:      cloneIntMap(stats.FilesSkippedByUser),
			FilesHidden:      stats.FilesSkippedHidden,
			FilesGitignore:   stats.FilesSkippedGitignore,
			FilesEshuIgnore:  stats.FilesSkippedEshuIgnore,
			TrackedFilesEshuIgnore: len(stats.TrackedFilesSkippedEshuIgnore) +
				stats.TrackedFilesSkippedEshuIgnoreOverflow,
		},
	}

	fileCounts := map[string]*collector.DiscoveryAdvisoryFile{}
	dirCounts := map[string]*collector.DiscoveryAdvisoryDirectory{}
	for _, file := range contentFiles {
		rel := filepath.ToSlash(file.RelativePath)
		dir := advisoryDir(rel)
		entry := dirEntry(dirCounts, dir)
		entry.IndexedFiles++
	}
	for _, entity := range entities {
		rel := filepath.ToSlash(entity.RelativePath)
		report.EntityCounts.ByType[entity.EntityType]++
		if entity.Language != "" {
			report.EntityCounts.ByLanguage[entity.Language]++
		}
		// Track entities by bounded source file kind (code, package_manifest,
		// config, other) so drainCollector can emit ContentEntityEmitted without
		// scanning individual facts. This is the counter that would have surfaced
		// the #3676 lockfile explosion instantly. The package-manifest signal is
		// the entity's config_kind metadata (not artifact_type, which the git
		// parser leaves empty for dependency manifests), keyed identically to the
		// reducer's package-manifest admission.
		kind := telemetry.ContentEntitySourceFileKind(
			entity.EntityType,
			entity.ArtifactType,
			entityConfigKind(entity.Metadata),
		)
		report.EntityCounts.BySourceFileKind[kind]++

		fileEntry := fileEntry(fileCounts, rel)
		fileEntry.ContentEntities++
		fileEntry.Language = entity.Language
		fileEntry.EntityTypes[entity.EntityType]++

		dir := advisoryDir(rel)
		dirEntry := dirEntry(dirCounts, dir)
		dirEntry.ContentEntities++
		dirEntry.EntityTypes[entity.EntityType]++
	}

	report.TopNoisyFiles = topAdvisoryFiles(fileCounts, 10)
	report.TopNoisyDirs = topAdvisoryDirs(dirCounts, 10)
	return report
}

// entityConfigKind returns the config_kind metadata value for a content entity,
// or "" when absent or non-string. The git dependency parsers set
// config_kind="dependency" on manifest dependency entities; this is the signal
// telemetry.ContentEntitySourceFileKind uses to classify package manifests,
// matching the reducer's package-manifest admission. The lookup is a single map
// read with no allocation, so it adds negligible cost to the advisory build.
func entityConfigKind(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	if value, ok := metadata["config_kind"].(string); ok {
		return value
	}
	return ""
}

func enrichDiscoveryAdvisoryRun(
	report *collector.DiscoveryAdvisoryReport,
	component string,
	repoID string,
	sourceRunID string,
	scopeID string,
	generationID string,
) {
	if report == nil {
		return
	}
	report.Run.Component = component
	report.Run.RepoID = repoID
	report.Run.SourceRunID = sourceRunID
	report.Run.ScopeID = scopeID
	report.Run.GenerationID = generationID
}

func fileEntry(entries map[string]*collector.DiscoveryAdvisoryFile, path string) *collector.DiscoveryAdvisoryFile {
	entry := entries[path]
	if entry == nil {
		entry = &collector.DiscoveryAdvisoryFile{Path: path, EntityTypes: map[string]int{}}
		entries[path] = entry
	}
	return entry
}

func dirEntry(entries map[string]*collector.DiscoveryAdvisoryDirectory, path string) *collector.DiscoveryAdvisoryDirectory {
	entry := entries[path]
	if entry == nil {
		entry = &collector.DiscoveryAdvisoryDirectory{Path: path, EntityTypes: map[string]int{}}
		entries[path] = entry
	}
	return entry
}

func advisoryDir(path string) string {
	dir := filepath.ToSlash(filepath.Dir(path))
	if dir == "." {
		return ""
	}
	return dir
}

func topAdvisoryFiles(entries map[string]*collector.DiscoveryAdvisoryFile, limit int) []collector.DiscoveryAdvisoryFile {
	items := make([]collector.DiscoveryAdvisoryFile, 0, len(entries))
	for _, entry := range entries {
		if entry.ContentEntities == 0 {
			continue
		}
		items = append(items, *entry)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ContentEntities == items[j].ContentEntities {
			return items[i].Path < items[j].Path
		}
		return items[i].ContentEntities > items[j].ContentEntities
	})
	return capAdvisoryFiles(items, limit)
}

func topAdvisoryDirs(entries map[string]*collector.DiscoveryAdvisoryDirectory, limit int) []collector.DiscoveryAdvisoryDirectory {
	items := make([]collector.DiscoveryAdvisoryDirectory, 0, len(entries))
	for _, entry := range entries {
		if entry.ContentEntities == 0 && entry.IndexedFiles == 0 {
			continue
		}
		items = append(items, *entry)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ContentEntities == items[j].ContentEntities {
			if items[i].IndexedFiles == items[j].IndexedFiles {
				return items[i].Path < items[j].Path
			}
			return items[i].IndexedFiles > items[j].IndexedFiles
		}
		return items[i].ContentEntities > items[j].ContentEntities
	})
	return capAdvisoryDirs(items, limit)
}

func capAdvisoryFiles(items []collector.DiscoveryAdvisoryFile, limit int) []collector.DiscoveryAdvisoryFile {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func capAdvisoryDirs(items []collector.DiscoveryAdvisoryDirectory, limit int) []collector.DiscoveryAdvisoryDirectory {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func cloneIntMap(input map[string]int) map[string]int {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]int, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
