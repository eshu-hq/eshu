package collector

import (
	"path/filepath"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
)

const discoveryAdvisorySchemaVersion = "discovery_advisory.v1"

// DiscoveryAdvisoryReport is an operator-facing JSON-safe summary of the repo
// discovery and materialization shape that made one index run cheap or noisy.
type DiscoveryAdvisoryReport struct {
	SchemaVersion string                         `json:"schema_version"`
	GeneratedAt   time.Time                      `json:"generated_at"`
	Run           DiscoveryAdvisoryRun           `json:"run"`
	Summary       DiscoveryAdvisorySummary       `json:"summary"`
	TopNoisyDirs  []DiscoveryAdvisoryDirectory   `json:"top_noisy_directories,omitempty"`
	TopNoisyFiles []DiscoveryAdvisoryFile        `json:"top_noisy_files,omitempty"`
	EntityCounts  DiscoveryAdvisoryEntityCount   `json:"entity_counts"`
	SkipBreakdown DiscoveryAdvisorySkipBreakdown `json:"skip_breakdown"`
}

// DiscoveryAdvisoryRun identifies the run/scope context for one advisory.
type DiscoveryAdvisoryRun struct {
	Component    string `json:"component,omitempty"`
	RepoID       string `json:"repo_id,omitempty"`
	RepoPath     string `json:"repo_path"`
	SourceRunID  string `json:"source_run_id,omitempty"`
	ScopeID      string `json:"scope_id,omitempty"`
	GenerationID string `json:"generation_id,omitempty"`
	CommitSHA    string `json:"commit_sha,omitempty"`
}

// DiscoveryAdvisorySummary contains low-cardinality counts for one snapshot.
type DiscoveryAdvisorySummary struct {
	DiscoveredFiles   int `json:"discovered_files"`
	ParsedFiles       int `json:"parsed_files"`
	ParseSkippedFiles int `json:"parse_skipped_files"`
	ContentFiles      int `json:"content_files"`
	ContentEntities   int `json:"content_entities"`
	SkippedDirs       int `json:"skipped_dirs"`
	SkippedFiles      int `json:"skipped_files"`
}

// DiscoveryAdvisoryDirectory reports the noisiest indexed directories by
// materialized entity count.
type DiscoveryAdvisoryDirectory struct {
	Path            string         `json:"path"`
	IndexedFiles    int            `json:"indexed_files"`
	ContentEntities int            `json:"content_entities"`
	EntityTypes     map[string]int `json:"entity_types,omitempty"`
}

// DiscoveryAdvisoryFile reports the noisiest indexed files by entity count.
type DiscoveryAdvisoryFile struct {
	Path            string         `json:"path"`
	ContentEntities int            `json:"content_entities"`
	Language        string         `json:"language,omitempty"`
	EntityTypes     map[string]int `json:"entity_types,omitempty"`
}

// DiscoveryAdvisoryEntityCount reports entity cardinality by type/language.
type DiscoveryAdvisoryEntityCount struct {
	ByType     map[string]int `json:"by_type,omitempty"`
	ByLanguage map[string]int `json:"by_language,omitempty"`
}

// DiscoveryAdvisorySkipBreakdown mirrors discovery skip telemetry without
// putting paths into metrics.
type DiscoveryAdvisorySkipBreakdown struct {
	DirsByName       map[string]int `json:"dirs_by_name,omitempty"`
	DirsByUser       map[string]int `json:"dirs_by_user,omitempty"`
	FilesByExtension map[string]int `json:"files_by_extension,omitempty"`
	FilesByContent   map[string]int `json:"files_by_content,omitempty"`
	FilesByUser      map[string]int `json:"files_by_user,omitempty"`
	FilesHidden      int            `json:"files_hidden,omitempty"`
	FilesGitignore   int            `json:"files_gitignore,omitempty"`
	FilesEshuIgnore  int            `json:"files_eshuignore,omitempty"`
}

func buildDiscoveryAdvisoryReport(
	repoPath string,
	generatedAt time.Time,
	stats discovery.DiscoveryStats,
	discoveredFiles []string,
	contentFiles []ContentFileMeta,
	entities []ContentEntitySnapshot,
	commitSHA string,
) *DiscoveryAdvisoryReport {
	contentFileCount := len(contentFiles)
	report := &DiscoveryAdvisoryReport{
		SchemaVersion: discoveryAdvisorySchemaVersion,
		GeneratedAt:   generatedAt,
		Run: DiscoveryAdvisoryRun{
			RepoPath:  repoPath,
			CommitSHA: commitSHA,
		},
		Summary: DiscoveryAdvisorySummary{
			DiscoveredFiles:   len(discoveredFiles),
			ParsedFiles:       contentFileCount,
			ParseSkippedFiles: maxInt(len(discoveredFiles)-contentFileCount, 0),
			ContentFiles:      contentFileCount,
			ContentEntities:   len(entities),
			SkippedDirs:       stats.TotalDirsSkipped(),
			SkippedFiles:      stats.TotalFilesSkipped(),
		},
		EntityCounts: DiscoveryAdvisoryEntityCount{
			ByType:     map[string]int{},
			ByLanguage: map[string]int{},
		},
		SkipBreakdown: DiscoveryAdvisorySkipBreakdown{
			DirsByName:       cloneIntMap(stats.DirsSkippedByName),
			DirsByUser:       cloneIntMap(stats.DirsSkippedByUser),
			FilesByExtension: cloneIntMap(stats.FilesSkippedByExtension),
			FilesByContent:   cloneIntMap(stats.FilesSkippedByContent),
			FilesByUser:      cloneIntMap(stats.FilesSkippedByUser),
			FilesHidden:      stats.FilesSkippedHidden,
			FilesGitignore:   stats.FilesSkippedGitignore,
			FilesEshuIgnore:  stats.FilesSkippedEshuIgnore,
		},
	}

	fileCounts := map[string]*DiscoveryAdvisoryFile{}
	dirCounts := map[string]*DiscoveryAdvisoryDirectory{}
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

func enrichDiscoveryAdvisoryRun(
	report *DiscoveryAdvisoryReport,
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

func fileEntry(entries map[string]*DiscoveryAdvisoryFile, path string) *DiscoveryAdvisoryFile {
	entry := entries[path]
	if entry == nil {
		entry = &DiscoveryAdvisoryFile{Path: path, EntityTypes: map[string]int{}}
		entries[path] = entry
	}
	return entry
}

func dirEntry(entries map[string]*DiscoveryAdvisoryDirectory, path string) *DiscoveryAdvisoryDirectory {
	entry := entries[path]
	if entry == nil {
		entry = &DiscoveryAdvisoryDirectory{Path: path, EntityTypes: map[string]int{}}
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

func topAdvisoryFiles(entries map[string]*DiscoveryAdvisoryFile, limit int) []DiscoveryAdvisoryFile {
	items := make([]DiscoveryAdvisoryFile, 0, len(entries))
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

func topAdvisoryDirs(entries map[string]*DiscoveryAdvisoryDirectory, limit int) []DiscoveryAdvisoryDirectory {
	items := make([]DiscoveryAdvisoryDirectory, 0, len(entries))
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

func capAdvisoryFiles(items []DiscoveryAdvisoryFile, limit int) []DiscoveryAdvisoryFile {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func capAdvisoryDirs(items []DiscoveryAdvisoryDirectory, limit int) []DiscoveryAdvisoryDirectory {
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
