// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
	"github.com/eshu-hq/eshu/go/internal/content/shape"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const defaultSnapshotSCIPWorkers = 4

// SnapshotSCIPConfig captures the optional SCIP runtime contract for the Go collector.
type SnapshotSCIPConfig struct {
	Enabled   bool
	Languages []string
	Workers   int
	Indexer   scipProjectIndexer
	Parser    scipResultParser

	processLimiter chan struct{}
}

type scipProjectIndexer interface {
	IsAvailable(string) bool
	Run(context.Context, string, string, string) (string, error)
}

type scipResultParser interface {
	Parse(string, string) (parser.SCIPParseResult, error)
}

// LoadSnapshotSCIPConfig parses the SCIP environment contract for the Go collector.
func LoadSnapshotSCIPConfig(getenv func(string) string) SnapshotSCIPConfig {
	languages := defaultSnapshotSCIPLanguages()
	if raw := strings.TrimSpace(getenv("SCIP_LANGUAGES")); raw != "" {
		languages = languages[:0]
		for _, item := range strings.Split(raw, ",") {
			item = strings.TrimSpace(strings.ToLower(item))
			if item != "" {
				languages = append(languages, item)
			}
		}
	}
	workers := scipWorkersFromEnv(getenv("SCIP_WORKERS"))
	return SnapshotSCIPConfig{
		Enabled:        scipEnabledFromEnv(getenv("SCIP_INDEXER")),
		Languages:      languages,
		Workers:        workers,
		processLimiter: make(chan struct{}, workers),
	}
}

func defaultSnapshotSCIPLanguages() []string {
	return []string{"python", "typescript", "javascript", "go", "rust", "java", "cpp", "c"}
}

func scipEnabledFromEnv(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func scipWorkersFromEnv(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultSnapshotSCIPWorkers
	}
	workers, err := strconv.Atoi(raw)
	if err != nil || workers < 1 {
		return defaultSnapshotSCIPWorkers
	}
	return workers
}

func (c SnapshotSCIPConfig) acquireProcess(ctx context.Context) (func(), time.Duration, bool, error) {
	startedAt := time.Now()
	if c.processLimiter == nil {
		return func() {}, 0, false, nil
	}
	select {
	case c.processLimiter <- struct{}{}:
		return func() { <-c.processLimiter }, time.Since(startedAt), true, nil
	case <-ctx.Done():
		return nil, time.Since(startedAt), true, ctx.Err()
	}
}

func (s NativeRepositorySnapshotter) scipConfig() SnapshotSCIPConfig {
	return s.SCIP
}

func (s NativeRepositorySnapshotter) scipIndexer(config SnapshotSCIPConfig) scipProjectIndexer {
	if config.Indexer != nil {
		return config.Indexer
	}
	return parser.SCIPIndexer{}
}

func (s NativeRepositorySnapshotter) scipParser(config SnapshotSCIPConfig) scipResultParser {
	if config.Parser != nil {
		return config.Parser
	}
	return parser.SCIPIndexParser{}
}

func (s NativeRepositorySnapshotter) buildParsedRepositoryFiles(
	ctx context.Context,
	repoPath string,
	fileSet discovery.RepoFileSet,
	engine *parser.Engine,
	commitSHA string,
	isDependency bool,
	goPackageTargets parser.GoPackageSemanticRoots,
	repositoryID string,
) ([]shape.File, []map[string]any, []parseLanguageSummary, error) {
	if shapeFiles, parsedFiles, languageSummary, used, err := s.trySCIPSnapshot(
		ctx,
		repoPath,
		fileSet,
		engine,
		commitSHA,
		isDependency,
		goPackageTargets,
		repositoryID,
	); err != nil {
		return nil, nil, nil, err
	} else if used {
		return shapeFiles, parsedFiles, languageSummary, nil
	}

	if s.ParseWorkers <= 1 {
		return s.buildParsedRepositoryFilesSequential(ctx, repoPath, fileSet, engine, commitSHA, isDependency, goPackageTargets, repositoryID, nil)
	}
	return s.buildParsedRepositoryFilesConcurrent(ctx, repoPath, fileSet, engine, commitSHA, isDependency, goPackageTargets, repositoryID, nil)
}

func (s NativeRepositorySnapshotter) buildParsedRepositoryFilesSequential(
	ctx context.Context,
	repoPath string,
	fileSet discovery.RepoFileSet,
	engine *parser.Engine,
	commitSHA string,
	isDependency bool,
	goPackageTargets parser.GoPackageSemanticRoots,
	repositoryID string,
	scipFiles map[string]map[string]any,
) ([]shape.File, []map[string]any, []parseLanguageSummary, error) {
	results := make([]parseResult, 0, len(fileSet.Files))
	for index, filePath := range fileSet.Files {
		result := s.parseRepositoryFile(
			ctx,
			repoPath,
			parseFileJob{index: index, path: filePath},
			engine,
			commitSHA,
			isDependency,
			goPackageTargets,
			repositoryID,
			scipFiles,
		)
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		results = append(results, result)
	}
	return parseResultsToSnapshotFiles(len(fileSet.Files), results)
}

func fileParseDurationSeconds(startedAt time.Time) float64 {
	return time.Since(startedAt).Seconds()
}

type parseResult struct {
	index     int
	shapeFile shape.File
	parsed    map[string]any
	language  string
	duration  float64
	skipped   bool
}

func (s NativeRepositorySnapshotter) trySCIPSnapshot(
	ctx context.Context,
	repoPath string,
	fileSet discovery.RepoFileSet,
	engine *parser.Engine,
	commitSHA string,
	isDependency bool,
	goPackageTargets parser.GoPackageSemanticRoots,
	repositoryID string,
) ([]shape.File, []map[string]any, []parseLanguageSummary, bool, error) {
	config := s.scipConfig()
	if !config.Enabled {
		s.recordSCIPSnapshotAttempt(ctx, scipSnapshotLanguageUnknown, scipSnapshotResultDisabled)
		return nil, nil, nil, false, nil
	}

	groups := parser.DetectSCIPProjectLanguageGroups(fileSet.Files, config.Languages)
	if len(groups) == 0 {
		s.recordSCIPSnapshotAttempt(ctx, scipSnapshotLanguageUnknown, scipSnapshotResultNoLanguage)
		return nil, nil, nil, false, nil
	}
	indexer := s.scipIndexer(config)
	scipFiles, usedAny, err := s.collectSCIPLanguageGroupFiles(
		ctx,
		repoPath,
		groups,
		indexer,
		s.scipParser(config),
	)
	if err != nil {
		return nil, nil, nil, false, err
	}
	if !usedAny {
		return nil, nil, nil, false, nil
	}

	var (
		shapeFiles      []shape.File
		parsedFiles     []map[string]any
		languageSummary []parseLanguageSummary
	)
	if s.ParseWorkers <= 1 {
		shapeFiles, parsedFiles, languageSummary, err = s.buildParsedRepositoryFilesSequential(
			ctx,
			repoPath,
			fileSet,
			engine,
			commitSHA,
			isDependency,
			goPackageTargets,
			repositoryID,
			scipFiles,
		)
	} else {
		shapeFiles, parsedFiles, languageSummary, err = s.buildParsedRepositoryFilesConcurrent(
			ctx,
			repoPath,
			fileSet,
			engine,
			commitSHA,
			isDependency,
			goPackageTargets,
			repositoryID,
			scipFiles,
		)
	}
	if err != nil {
		return nil, nil, nil, false, err
	}

	if len(parsedFiles) == 0 {
		return nil, nil, nil, false, nil
	}
	return shapeFiles, parsedFiles, languageSummary, true, nil
}

func (s NativeRepositorySnapshotter) logSCIPSnapshotFallback(ctx context.Context, language string, reason string) {
	if s.Logger == nil {
		return
	}
	s.Logger.WarnContext(
		ctx, "SCIP snapshot fallback to native parser",
		log.Language(language),
		slog.String("reason", reason),
		telemetry.FailureClassAttr("scip_"+reason),
	)
}

func mergeSCIPSupplement(parsed map[string]any, supplement map[string]any) {
	if calls, ok := supplement["function_calls_scip"]; ok {
		parsed["function_calls_scip"] = calls
	}
}

func shapeFileFromParsed(parsed map[string]any, relativePath string, body string, commitSHA string) shape.File {
	return shape.File{
		Path:            relativePath,
		Body:            body,
		Digest:          digestForBody(body),
		Language:        snapshotPayloadString(parsed, "language", "lang"),
		ArtifactType:    snapshotPayloadString(parsed, "artifact_type"),
		TemplateDialect: snapshotPayloadString(parsed, "template_dialect"),
		IACRelevant:     snapshotPayloadBoolPtr(parsed, "iac_relevant"),
		CommitSHA:       commitSHA,
		EntityBuckets:   entityBucketsFromParsed(parsed),
	}
}
