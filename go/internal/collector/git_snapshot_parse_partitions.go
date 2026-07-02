// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

// defaultParseFileWeightBytes is the weight assumed for a file whose os.Stat fails.
// Unstattable files are never dropped from a partition; they are weighted at
// this default so balancing stays loss-free.
const defaultParseFileWeightBytes int64 = 4096

// minParseFileWeightBytes floors every file's parse weight so a large group of
// empty or tiny files still spreads across workers by count instead of
// collapsing into a single partition. Each file costs a fixed parse pass
// regardless of byte size, so weighting tiny files at zero would re-create the
// count-clustering this byte-balancing avoids.
const minParseFileWeightBytes int64 = 512

// minCostlyParserFileWeightBytes floors files handled by slower parser adapters.
// Full-corpus measurements show PHP, JavaScript, TypeScript, Python, Java, and
// SQL parse cost is often dominated by parser setup and AST traversal rather
// than raw bytes, so tiny files in those languages need enough weight to avoid
// clustering behind one worker.
const minCostlyParserFileWeightBytes int64 = 32 * 1024

var costlyParserExtensions = map[string]struct{}{
	".cjs":   {},
	".cts":   {},
	".ipynb": {},
	".java":  {},
	".js":    {},
	".jsx":   {},
	".mjs":   {},
	".mts":   {},
	".php":   {},
	".py":    {},
	".pyw":   {},
	".sql":   {},
	".ts":    {},
	".tsx":   {},
}

type parseFileJobSized struct {
	job    parseFileJob
	weight int64
}

// buildParseSubtreePartitions groups files by subtree key for parse context,
// then balances the partitions by estimated parse cost rather than file count
// so a subtree dominated by a few heavy or parser-expensive files does not pin
// one parse worker. The resulting partitions cover the exact same file set as
// the input — same
// indexes, no file dropped or duplicated — so the parse result is unchanged;
// only the worker distribution differs.
func buildParseSubtreePartitions(repoPath string, files []string, workerCount int) []parseSubtreePartition {
	if len(files) == 0 {
		return nil
	}

	groupOrder := make([]string, 0, len(files))
	groups := make(map[string][]parseFileJobSized)
	var totalWeight int64
	for index, filePath := range files {
		key := parseSubtreePartitionKey(repoPath, filePath)
		if _, ok := groups[key]; !ok {
			groupOrder = append(groupOrder, key)
		}
		weight := parseFileWeightBytes(filePath)
		totalWeight += weight
		groups[key] = append(groups[key], parseFileJobSized{
			job:    parseFileJob{index: index, path: filePath},
			weight: weight,
		})
	}
	sort.Strings(groupOrder)

	targetWeight := parseSubtreePartitionTargetWeight(totalWeight, workerCount)

	partitions := make([]parseSubtreePartition, 0, len(groupOrder))
	for _, key := range groupOrder {
		sized := groups[key]
		var groupWeight int64
		for _, item := range sized {
			groupWeight += item.weight
		}
		// Keep a group whole when its estimated parse cost fits within one
		// worker's target. Heavier groups are split so their cost spreads across
		// partitions.
		if groupWeight <= targetWeight {
			partitions = append(partitions, parseSubtreePartition{key: key, jobs: jobsFromSized(sized)})
			continue
		}
		partitions = append(partitions, chunkGroupByWeight(key, sized, targetWeight)...)
	}
	return partitions
}

// chunkGroupByWeight splits one subtree group into parse-cost-balanced chunks.
// A file is always placed in the current chunk first (so a single file larger
// than the target still lands in exactly one chunk, never dropped); a new chunk
// starts once the running total reaches the target and files remain.
func chunkGroupByWeight(key string, sized []parseFileJobSized, targetWeight int64) []parseSubtreePartition {
	partitions := make([]parseSubtreePartition, 0)
	current := make([]parseFileJob, 0, len(sized))
	var currentWeight int64
	chunk := 1
	flush := func() {
		partitions = append(partitions, parseSubtreePartition{
			key:  fmt.Sprintf("%s#%03d", key, chunk),
			jobs: current,
		})
		chunk++
		current = make([]parseFileJob, 0, len(sized))
		currentWeight = 0
	}
	for i, item := range sized {
		current = append(current, item.job)
		currentWeight += item.weight
		if currentWeight >= targetWeight && i < len(sized)-1 {
			flush()
		}
	}
	if len(current) > 0 {
		flush()
	}
	return partitions
}

func jobsFromSized(sized []parseFileJobSized) []parseFileJob {
	jobs := make([]parseFileJob, len(sized))
	for i, item := range sized {
		jobs[i] = item.job
	}
	return jobs
}

// parseFileWeightBytes returns the estimated parse cost of a file. It starts
// with on-disk size, applies a per-file floor, and uses a higher floor for
// parser adapters whose remote corpus timings show fixed per-file overhead.
// When os.Stat fails, it falls back to a default weight so an unstattable file
// is still scheduled (never dropped).
func parseFileWeightBytes(filePath string) int64 {
	info, err := os.Stat(filePath)
	if err != nil || info.Size() < 0 {
		return defaultParseFileWeightBytes
	}
	weight := max(info.Size(), minParseFileWeightBytes)
	if _, ok := costlyParserExtensions[strings.ToLower(filepath.Ext(filePath))]; ok {
		weight = max(weight, minCostlyParserFileWeightBytes)
	}
	return weight
}

func parseSubtreePartitionTargetWeight(totalWeight int64, workerCount int) int64 {
	if totalWeight <= 0 {
		return 1
	}
	if workerCount <= 1 {
		return totalWeight
	}
	return max(int64(1), (totalWeight+int64(workerCount)-1)/int64(workerCount))
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
