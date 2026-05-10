package collector

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
	"github.com/eshu-hq/eshu/go/internal/content/shape"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// SnapshotSCIPConfig captures the optional SCIP runtime contract for the Go collector.
type SnapshotSCIPConfig struct {
	Enabled   bool
	Languages []string
	Indexer   scipProjectIndexer
	Parser    scipResultParser
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
	languages := []string{"python", "typescript", "go", "rust", "java"}
	if raw := strings.TrimSpace(getenv("SCIP_LANGUAGES")); raw != "" {
		languages = languages[:0]
		for _, item := range strings.Split(raw, ",") {
			item = strings.TrimSpace(strings.ToLower(item))
			if item != "" {
				languages = append(languages, item)
			}
		}
	}
	return SnapshotSCIPConfig{
		Enabled:   boolFromEnv(getenv("SCIP_INDEXER")),
		Languages: languages,
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
) ([]shape.File, []map[string]any, error) {
	if shapeFiles, parsedFiles, used, err := s.trySCIPSnapshot(
		ctx,
		repoPath,
		fileSet,
		engine,
		commitSHA,
		isDependency,
		goPackageTargets,
	); err != nil {
		return nil, nil, err
	} else if used {
		return shapeFiles, parsedFiles, nil
	}

	if s.ParseWorkers <= 1 {
		return s.buildParsedRepositoryFilesSequential(ctx, repoPath, fileSet, engine, commitSHA, isDependency, goPackageTargets)
	}
	return s.buildParsedRepositoryFilesConcurrent(ctx, repoPath, fileSet, engine, commitSHA, isDependency, goPackageTargets)
}

func (s NativeRepositorySnapshotter) buildParsedRepositoryFilesSequential(
	ctx context.Context,
	repoPath string,
	fileSet discovery.RepoFileSet,
	engine *parser.Engine,
	commitSHA string,
	isDependency bool,
	goPackageTargets parser.GoPackageSemanticRoots,
) ([]shape.File, []map[string]any, error) {
	shapeFiles := make([]shape.File, 0, len(fileSet.Files))
	parsedFiles := make([]map[string]any, 0, len(fileSet.Files))
	for _, filePath := range fileSet.Files {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		startTime := time.Now()
		parsed, err := engine.ParsePath(repoPath, filePath, isDependency, snapshotParserOptions(filePath, goPackageTargets))
		duration := fileParseDurationSeconds(startTime)

		if err != nil {
			// Skip files that cannot be parsed (e.g. malformed JSON,
			// binary files with recognized extensions). A single bad
			// file must not prevent the rest of the repository from
			// being indexed.
			if s.Instruments != nil {
				s.Instruments.FilesParsed.Add(ctx, 1, metric.WithAttributes(
					attribute.String("status", "skipped"),
				))
			}
			continue
		}

		body, err := os.ReadFile(filePath)
		if err != nil {
			if s.Instruments != nil {
				s.Instruments.FilesParsed.Add(ctx, 1, metric.WithAttributes(
					attribute.String("status", "skipped"),
				))
			}
			continue
		}

		relativePath, err := filepath.Rel(repoPath, filePath)
		if err != nil {
			if s.Instruments != nil {
				s.Instruments.FilesParsed.Add(ctx, 1, metric.WithAttributes(
					attribute.String("status", "skipped"),
				))
			}
			continue
		}
		relativePath = filepath.ToSlash(filepath.Clean(relativePath))
		shapeFiles = append(shapeFiles, shapeFileFromParsed(parsed, relativePath, string(body), commitSHA))
		parsedFiles = append(parsedFiles, parsed)

		if s.Instruments != nil {
			language := snapshotPayloadString(parsed, "language", "lang")
			s.Instruments.FileParseDuration.Record(ctx, duration, metric.WithAttributes(
				attribute.String("language", language),
			))
			s.Instruments.FilesParsed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("status", "succeeded"),
			))
		}
	}
	return shapeFiles, parsedFiles, nil
}

func fileParseDurationSeconds(startedAt time.Time) float64 {
	return time.Since(startedAt).Seconds()
}

type parseResult struct {
	index     int
	shapeFile shape.File
	parsed    map[string]any
	skipped   bool
}

func (s NativeRepositorySnapshotter) buildParsedRepositoryFilesConcurrent(
	ctx context.Context,
	repoPath string,
	fileSet discovery.RepoFileSet,
	engine *parser.Engine,
	commitSHA string,
	isDependency bool,
	goPackageTargets parser.GoPackageSemanticRoots,
) ([]shape.File, []map[string]any, error) {
	fileCount := len(fileSet.Files)
	if fileCount == 0 {
		return nil, nil, nil
	}

	// Create cancellable context for workers (only cancel on context error, not parse errors)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create channels
	type fileJob struct {
		index int
		path  string
	}
	jobs := make(chan fileJob, fileCount)
	results := make(chan parseResult, fileCount)

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < s.ParseWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				// Check context
				if err := workerCtx.Err(); err != nil {
					results <- parseResult{index: job.index, skipped: true}
					continue
				}

				startTime := time.Now()
				parsed, err := engine.ParsePath(repoPath, job.path, isDependency, snapshotParserOptions(job.path, goPackageTargets))
				duration := fileParseDurationSeconds(startTime)

				if err != nil {
					// Parse error: skip file but continue processing others
					if s.Instruments != nil {
						s.Instruments.FilesParsed.Add(workerCtx, 1, metric.WithAttributes(
							attribute.String("status", "skipped"),
						))
					}
					results <- parseResult{index: job.index, skipped: true}
					continue
				}

				body, err := os.ReadFile(job.path)
				if err != nil {
					if s.Instruments != nil {
						s.Instruments.FilesParsed.Add(workerCtx, 1, metric.WithAttributes(
							attribute.String("status", "skipped"),
						))
					}
					results <- parseResult{index: job.index, skipped: true}
					continue
				}

				relativePath, err := filepath.Rel(repoPath, job.path)
				if err != nil {
					if s.Instruments != nil {
						s.Instruments.FilesParsed.Add(workerCtx, 1, metric.WithAttributes(
							attribute.String("status", "skipped"),
						))
					}
					results <- parseResult{index: job.index, skipped: true}
					continue
				}
				relativePath = filepath.ToSlash(filepath.Clean(relativePath))

				if s.Instruments != nil {
					language := snapshotPayloadString(parsed, "language", "lang")
					s.Instruments.FileParseDuration.Record(workerCtx, duration, metric.WithAttributes(
						attribute.String("language", language),
					))
					s.Instruments.FilesParsed.Add(workerCtx, 1, metric.WithAttributes(
						attribute.String("status", "succeeded"),
					))
				}

				results <- parseResult{
					index:     job.index,
					shapeFile: shapeFileFromParsed(parsed, relativePath, string(body), commitSHA),
					parsed:    parsed,
					skipped:   false,
				}
			}
		}()
	}

	// Send jobs
	for i, filePath := range fileSet.Files {
		jobs <- fileJob{index: i, path: filePath}
	}
	close(jobs)

	// Wait for workers to finish
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	resultSlice := make([]parseResult, 0, fileCount)
	for result := range results {
		resultSlice = append(resultSlice, result)
	}

	// Check for context error
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	// Sort by original index to preserve deterministic order
	sort.Slice(resultSlice, func(i, j int) bool {
		return resultSlice[i].index < resultSlice[j].index
	})

	// Extract successful results in original order
	shapeFiles := make([]shape.File, 0, fileCount)
	parsedFiles := make([]map[string]any, 0, fileCount)
	for _, result := range resultSlice {
		if !result.skipped {
			shapeFiles = append(shapeFiles, result.shapeFile)
			parsedFiles = append(parsedFiles, result.parsed)
		}
	}

	return shapeFiles, parsedFiles, nil
}

func (s NativeRepositorySnapshotter) trySCIPSnapshot(
	ctx context.Context,
	repoPath string,
	fileSet discovery.RepoFileSet,
	engine *parser.Engine,
	commitSHA string,
	isDependency bool,
	goPackageTargets parser.GoPackageSemanticRoots,
) ([]shape.File, []map[string]any, bool, error) {
	config := s.scipConfig()
	if !config.Enabled {
		return nil, nil, false, nil
	}

	language := parser.DetectSCIPProjectLanguage(fileSet.Files, config.Languages)
	if language == "" {
		return nil, nil, false, nil
	}
	indexer := s.scipIndexer(config)
	if !indexer.IsAvailable(language) {
		return nil, nil, false, nil
	}

	outputDir, err := os.MkdirTemp("", "eshu-scip-*")
	if err != nil {
		return nil, nil, false, err
	}
	defer func() {
		_ = os.RemoveAll(outputDir)
	}()

	indexPath, err := indexer.Run(ctx, repoPath, language, outputDir)
	if err != nil {
		return nil, nil, false, nil
	}
	result, err := s.scipParser(config).Parse(indexPath, repoPath)
	if err != nil {
		return nil, nil, false, nil
	}

	selectedFiles := make(map[string]struct{}, len(fileSet.Files))
	for _, path := range fileSet.Files {
		selectedFiles[path] = struct{}{}
	}
	orderedFiles := make([]string, 0, len(result.Files))
	for path := range result.Files {
		if _, ok := selectedFiles[path]; ok {
			orderedFiles = append(orderedFiles, path)
		}
	}
	slices.Sort(orderedFiles)

	shapeFiles := make([]shape.File, 0, len(orderedFiles))
	parsedFiles := make([]map[string]any, 0, len(orderedFiles))
	for _, filePath := range orderedFiles {
		if err := ctx.Err(); err != nil {
			return nil, nil, false, err
		}

		parsed := clonePayload(result.Files[filePath])
		supplement, err := engine.ParsePath(repoPath, filePath, isDependency, snapshotParserOptions(filePath, goPackageTargets))
		if err != nil {
			return nil, nil, false, nil
		}
		mergeSCIPSupplement(parsed, supplement)
		parsed["repo_path"] = repoPath

		body, err := os.ReadFile(filePath)
		if err != nil {
			return nil, nil, false, nil
		}
		relativePath, err := filepath.Rel(repoPath, filePath)
		if err != nil {
			return nil, nil, false, err
		}
		relativePath = filepath.ToSlash(filepath.Clean(relativePath))
		shapeFiles = append(shapeFiles, shapeFileFromParsed(parsed, relativePath, string(body), commitSHA))
		parsedFiles = append(parsedFiles, parsed)
	}

	if len(parsedFiles) == 0 {
		return nil, nil, false, nil
	}
	return shapeFiles, parsedFiles, true, nil
}

func mergeSCIPSupplement(parsed map[string]any, supplement map[string]any) {
	parsed["imports"] = supplement["imports"]
	parsed["variables"] = supplement["variables"]
	parsed["functions"] = mergeNamedEntities(
		parsed["functions"],
		supplement["functions"],
		[]string{"source", "cyclomatic_complexity", "decorators", "context", "class_context", "dead_code_root_kinds", "start_byte", "end_byte", "end_line"},
	)
	parsed["classes"] = mergeNamedEntities(
		parsed["classes"],
		supplement["classes"],
		[]string{"bases", "context", "source", "start_byte", "end_byte", "end_line"},
	)
}

func mergeNamedEntities(current any, supplement any, keys []string) []map[string]any {
	currentItems, _ := current.([]map[string]any)
	supplementItems, _ := supplement.([]map[string]any)
	byName := make(map[string]map[string]any, len(supplementItems))
	for _, item := range supplementItems {
		name := snapshotPayloadString(item, "name")
		if name != "" {
			byName[name] = item
		}
	}
	for i := range currentItems {
		name := snapshotPayloadString(currentItems[i], "name")
		supplementItem, ok := byName[name]
		if !ok {
			continue
		}
		for _, key := range keys {
			if value, ok := supplementItem[key]; ok {
				currentItems[i][key] = value
			}
		}
	}
	return currentItems
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

func clonePayload(values map[string]any) map[string]any {
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
