package collector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

type parseFileJob struct {
	index int
	path  string
}

type parseSubtreePartition struct {
	key  string
	jobs []parseFileJob
}

func buildParseSubtreePartitions(repoPath string, files []string, workerCount int) []parseSubtreePartition {
	if len(files) == 0 {
		return nil
	}
	targetSize := parseSubtreePartitionTargetSize(len(files), workerCount)
	groupOrder := make([]string, 0, len(files))
	groups := make(map[string][]parseFileJob)
	for index, filePath := range files {
		key := parseSubtreePartitionKey(repoPath, filePath)
		if _, ok := groups[key]; !ok {
			groupOrder = append(groupOrder, key)
		}
		groups[key] = append(groups[key], parseFileJob{index: index, path: filePath})
	}
	sort.Strings(groupOrder)

	partitions := make([]parseSubtreePartition, 0, len(groupOrder))
	for _, key := range groupOrder {
		jobs := groups[key]
		if len(jobs) <= targetSize {
			partitions = append(partitions, parseSubtreePartition{key: key, jobs: jobs})
			continue
		}
		for start, chunk := 0, 1; start < len(jobs); start, chunk = start+targetSize, chunk+1 {
			end := min(start+targetSize, len(jobs))
			partitions = append(partitions, parseSubtreePartition{
				key:  fmt.Sprintf("%s#%03d", key, chunk),
				jobs: jobs[start:end],
			})
		}
	}
	return partitions
}

func parseSubtreePartitionTargetSize(fileCount int, workerCount int) int {
	if fileCount <= 0 {
		return 1
	}
	if workerCount <= 1 {
		return fileCount
	}
	return max(1, (fileCount+workerCount-1)/workerCount)
}

func parseSubtreePartitionKey(repoPath string, filePath string) string {
	rel, err := filepath.Rel(filepath.Clean(repoPath), filepath.Clean(filePath))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "."
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	parts := strings.Split(rel, "/")
	switch {
	case len(parts) <= 1:
		return "."
	case len(parts) == 2:
		return parts[0]
	default:
		return parts[0] + "/" + parts[1]
	}
}

func (s NativeRepositorySnapshotter) buildParsedRepositoryFilesConcurrent(
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
	fileCount := len(fileSet.Files)
	if fileCount == 0 {
		return nil, nil, nil, nil
	}

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	partitions := buildParseSubtreePartitions(repoPath, fileSet.Files, s.ParseWorkers)
	jobs := make(chan parseSubtreePartition, len(partitions))
	results := make(chan []parseResult, len(partitions))

	var wg sync.WaitGroup
	workerCount := min(max(1, s.ParseWorkers), len(partitions))
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for partition := range jobs {
				results <- s.parseRepositoryFilePartition(
					workerCtx,
					repoPath,
					partition,
					engine,
					commitSHA,
					isDependency,
					goPackageTargets,
					repositoryID,
					scipFiles,
				)
			}
		}()
	}

	for _, partition := range partitions {
		jobs <- partition
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	resultSlice := make([]parseResult, 0, fileCount)
	for partitionResults := range results {
		resultSlice = append(resultSlice, partitionResults...)
	}

	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}

	sort.Slice(resultSlice, func(i, j int) bool {
		return resultSlice[i].index < resultSlice[j].index
	})

	return parseResultsToSnapshotFiles(fileCount, resultSlice)
}

func (s NativeRepositorySnapshotter) parseRepositoryFilePartition(
	ctx context.Context,
	repoPath string,
	partition parseSubtreePartition,
	engine *parser.Engine,
	commitSHA string,
	isDependency bool,
	goPackageTargets parser.GoPackageSemanticRoots,
	repositoryID string,
	scipFiles map[string]map[string]any,
) []parseResult {
	results := make([]parseResult, 0, len(partition.jobs))
	for _, job := range partition.jobs {
		results = append(results, s.parseRepositoryFile(
			ctx,
			repoPath,
			job,
			engine,
			commitSHA,
			isDependency,
			goPackageTargets,
			repositoryID,
			scipFiles,
		))
	}
	return results
}

func (s NativeRepositorySnapshotter) parseRepositoryFile(
	ctx context.Context,
	repoPath string,
	job parseFileJob,
	engine *parser.Engine,
	commitSHA string,
	isDependency bool,
	goPackageTargets parser.GoPackageSemanticRoots,
	repositoryID string,
	scipFiles map[string]map[string]any,
) parseResult {
	if err := ctx.Err(); err != nil {
		return parseResult{index: job.index, skipped: true}
	}

	startTime := time.Now()
	parsed, err := engine.ParsePath(
		repoPath,
		job.path,
		isDependency,
		snapshotParserOptions(job.path, goPackageTargets, s.EmitDataflow, repositoryID),
	)
	duration := fileParseDurationSeconds(startTime)
	if err != nil {
		s.recordParseFileStatus(ctx, "skipped")
		return parseResult{index: job.index, skipped: true}
	}
	if scipPayload, ok := scipFiles[job.path]; ok {
		mergeSCIPSupplement(parsed, scipPayload)
	}

	body, err := os.ReadFile(job.path)
	if err != nil {
		s.recordParseFileStatus(ctx, "skipped")
		return parseResult{index: job.index, skipped: true}
	}

	relativePath, err := filepath.Rel(repoPath, job.path)
	if err != nil {
		s.recordParseFileStatus(ctx, "skipped")
		return parseResult{index: job.index, skipped: true}
	}
	relativePath = filepath.ToSlash(filepath.Clean(relativePath))
	language := snapshotPayloadString(parsed, "language", "lang")

	if s.Instruments != nil {
		s.Instruments.FileParseDuration.Record(ctx, duration, metric.WithAttributes(
			attribute.String("language", language),
		))
	}
	s.recordParseFileStatus(ctx, "succeeded")

	return parseResult{
		index:     job.index,
		shapeFile: shapeFileFromParsed(parsed, relativePath, string(body), commitSHA),
		parsed:    parsed,
		language:  language,
		duration:  duration,
		skipped:   false,
	}
}

func (s NativeRepositorySnapshotter) recordParseFileStatus(ctx context.Context, status string) {
	if s.Instruments == nil {
		return
	}
	s.Instruments.FilesParsed.Add(ctx, 1, metric.WithAttributes(
		attribute.String("status", status),
	))
}

func parseResultsToSnapshotFiles(
	fileCount int,
	resultSlice []parseResult,
) ([]shape.File, []map[string]any, []parseLanguageSummary, error) {
	shapeFiles := make([]shape.File, 0, fileCount)
	parsedFiles := make([]map[string]any, 0, fileCount)
	languageStats := newParseLanguageStats()
	for _, result := range resultSlice {
		if result.skipped {
			continue
		}
		shapeFiles = append(shapeFiles, result.shapeFile)
		parsedFiles = append(parsedFiles, result.parsed)
		languageStats.record(result.language, result.duration)
	}
	return shapeFiles, parsedFiles, languageStats.summaries(), nil
}
