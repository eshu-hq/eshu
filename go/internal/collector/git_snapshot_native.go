// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/content/shape"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// NativeRepositorySnapshotter builds repository snapshots without Python bridge code.
type NativeRepositorySnapshotter struct {
	Engine           *parser.Engine
	Registry         parser.Registry
	DiscoveryOptions discovery.Options
	SCIP             SnapshotSCIPConfig
	Now              func() time.Time
	ParseWorkers     int
	// EmitDataflow opts the per-file parser into emitting value-flow buckets
	// (dataflow_functions, taint_findings, interproc_findings). Off by default;
	// the snapshot payload is byte-identical when off.
	EmitDataflow bool
	Tracer       trace.Tracer
	Instruments  *telemetry.Instruments
	Logger       *slog.Logger
}

// SnapshotRepository builds one native repository snapshot for the selected repo.
func (s NativeRepositorySnapshotter) SnapshotRepository(
	ctx context.Context,
	repository SelectedRepository,
) (RepositorySnapshot, error) {
	repoPath, err := filepath.Abs(repository.RepoPath)
	if err != nil {
		return RepositorySnapshot{}, fmt.Errorf("resolve repository path %q: %w", repository.RepoPath, err)
	}
	if resolvedPath, resolveErr := filepath.EvalSymlinks(repoPath); resolveErr == nil {
		repoPath = resolvedPath
	}
	gitTreePath := strings.TrimSpace(repository.GitTreePath)
	if gitTreePath == "" {
		gitTreePath = repoPath
	} else {
		gitTreePath = canonicalLocalPath(gitTreePath)
	}

	engine, err := s.engine()
	if err != nil {
		return RepositorySnapshot{}, err
	}
	registry := s.registry()
	discoveryStartedAt := time.Now()
	fullFileSet, discoveryStats, err := resolveNativeSnapshotFileSet(repoPath, registry, s.discoveryOptions())
	fileSet := fullFileSet
	if len(repository.FileTargets) > 0 {
		fileSet, err = resolveNativeSnapshotFileSetForTargets(repoPath, repository.FileTargets, registry)
	}
	if err != nil {
		return RepositorySnapshot{}, err
	}
	deltaChangedRelativePaths := relativePathsForSnapshotTargets(repoPath, repository.FileTargets)
	deltaDeletedRelativePaths := normalizeSnapshotRelativePaths(repository.DeletedRelativePaths)
	deltaRelativePaths := sortUniquePathStrings(append(deltaChangedRelativePaths, deltaDeletedRelativePaths...))
	var tfstateCandidates []TerraformStateCandidate
	fileSet.Files, tfstateCandidates = extractTerraformStateCandidates(repoPath, fileSet.Files)
	// .gitmodules is diverted the same way tfstate candidates are: removed
	// from fileSet.Files before the language-parser pipeline sees it, since
	// .gitmodules has no file extension and no registered parser.Definition.
	// Its ContentFileMeta entry is built directly from a content digest below
	// (see gitmodulesFileMetasForPaths), not through shape.Materialize.
	var gitmodulesCandidateRelativePaths []string
	fileSet.Files, gitmodulesCandidateRelativePaths = extractGitmodulesCandidateFiles(repoPath, fileSet.Files)
	// CODEOWNERS candidates are diverted the same way tfstate candidates are:
	// removed from fileSet.Files before the language-parser pipeline sees
	// them, since CODEOWNERS has no file extension and no registered
	// parser.Definition. Their ContentFileMeta entries are built directly
	// from a content digest below (see codeownersFileMetasForPaths), not
	// through shape.Materialize.
	var codeownersCandidateRelativePaths []string
	fileSet.Files, codeownersCandidateRelativePaths = extractCodeownersCandidateFiles(repoPath, fileSet.Files)
	// See resolvedCodeownersCandidateRelativePaths (issue #5419 P1): a delta
	// touching a CODEOWNERS candidate re-reads every candidate from repoPath.
	codeownersCandidateRelativePaths = resolvedCodeownersCandidateRelativePaths(repoPath, repository.Delta, deltaRelativePaths, codeownersCandidateRelativePaths)
	parserFiles, documentationFiles := partitionNativeSnapshotFiles(fileSet.Files, registry)
	fullParserFiles, _ := partitionNativeSnapshotFiles(fullFileSet.Files, registry)
	parserFileSet := fileSet
	parserFileSet.Files = parserFiles
	preScanFileSet := parserFileSet
	preScanFileSet.Files = sortUniqueFileWithSizeSlice(append(
		parserPreScanFiles(fullParserFiles),
		parserPreScanFiles(parserFileSet.Files)...,
	))
	logTerraformStateCandidateDiscovery(ctx, s, repoPath, len(tfstateCandidates))
	s.logDiscoveryStats(ctx, repoPath, discoveryStats)
	s.recordSnapshotStage(
		ctx, repoPath, telemetry.SnapshotStageDiscovery, discoveryStartedAt,
		slog.Int("file_count", len(fileSet.Files)),
		slog.Int("file_target_count", len(repository.FileTargets)),
	)
	s.recordDiscoveryMetrics(ctx, discoveryStats)

	snapshot := RepositorySnapshot{
		RepoPath:                 repoPath,
		GitTreePath:              gitTreePath,
		RemoteURL:                repository.RemoteURL,
		FileCount:                len(fileSet.Files),
		ImportsMap:               map[string][]string{},
		FileData:                 []map[string]any{},
		ContentFiles:             []ContentFileSnapshot{},
		ContentEntities:          []ContentEntitySnapshot{},
		TerraformStateCandidates: tfstateCandidates,
		Delta:                    repository.Delta,
		DeltaRelativePaths:       deltaRelativePaths,
		DeletedRelativePaths:     deltaDeletedRelativePaths,
		Reconcile:                repository.Reconcile,
	}
	commitSHA := repository.SourceCommitSHA
	if commitSHA == "" {
		commitSHA = gitCommitSHAFn(ctx, repoPath)
	}
	snapshot.HeadCommitSHA = commitSHA
	// Built here (before the zero-remaining-files early return) so a
	// repository whose only discovered file is .gitmodules or a CODEOWNERS
	// candidate still carries it, mirroring how TerraformStateCandidates
	// above survives that same early return. Both candidate sets are
	// appended together since a repository can carry both a .gitmodules file
	// and a CODEOWNERS file in the same generation.
	snapshot.ContentFileMetas = append(
		gitmodulesFileMetasForPaths(repoPath, gitmodulesCandidateRelativePaths, commitSHA),
		codeownersFileMetasForPaths(repoPath, codeownersCandidateRelativePaths, commitSHA)...,
	)
	if len(fileSet.Files) == 0 {
		snapshot.DiscoveryAdvisory = buildDiscoveryAdvisoryReport(
			repoPath,
			s.now(),
			discoveryStats,
			fileSet.FilePaths(),
			snapshot.ContentFileMetas,
			nil,
			commitSHA,
		)
		return snapshot, nil
	}

	// deriveImportsMapFromParse is only safe when this generation's parse
	// stage covers the exact same file set pre-scan needs. On a delta sync
	// (FileTargets set), pre-scan still spans the full repo
	// (parserPreScanFiles(fullParserFiles) above) while parse only visits the
	// changed targets, so deriving from parse output would silently drop
	// ImportsMap entries for every unchanged derive-eligible file. See
	// partitionPreScanFilesForDerive's doc comment.
	deriveImportsMapFromParse := len(repository.FileTargets) == 0
	legacyPreScanFiles, deriveEligiblePreScanFiles := partitionPreScanFilesForDerive(
		preScanFileSet.Files, registry, deriveImportsMapFromParse,
	)

	preScanStartedAt := time.Now()
	importsMap, preScanFileStats, err := engine.PreScanRepositoryPathsWithWorkersStats(
		repoPath,
		discovery.FilePaths(legacyPreScanFiles),
		effectiveSnapshotParseWorkers(s.ParseWorkers),
	)
	if err != nil {
		return RepositorySnapshot{}, fmt.Errorf("pre-scan repository imports for %q: %w", repoPath, err)
	}
	// Capture the stage end time before building the per-language summary:
	// preScanLanguageSummary records one histogram sample per pre-scanned file,
	// and that recording cost must not be attributed to the pre_scan stage
	// duration itself (#4767).
	preScanEndedAt := time.Now()
	preScanSummary := preScanLanguageSummary(ctx, s, preScanFileStats)
	s.recordSnapshotStageAt(
		ctx, repoPath, telemetry.SnapshotStagePreScan, preScanStartedAt, preScanEndedAt,
		slog.Int("file_count", len(legacyPreScanFiles)),
		slog.Int("import_symbol_count", len(importsMap)),
		slog.Int("pre_scan_workers", effectiveSnapshotParseWorkers(s.ParseWorkers)),
		slog.Int("derive_from_parse_file_count", len(deriveEligiblePreScanFiles)),
		slog.Any("language_prescan_summary", preScanSummary),
	)
	goPackageSemanticPreScanStartedAt := time.Now()
	goPackageTargets, err := engine.PreScanGoPackageSemanticRoots(repoPath, preScanFileSet.FilePaths())
	if err != nil {
		return RepositorySnapshot{}, fmt.Errorf("pre-scan go package interface params for %q: %w", repoPath, err)
	}
	s.recordSnapshotStage(
		ctx, repoPath, telemetry.SnapshotStageGoPackageSemanticPreScan, goPackageSemanticPreScanStartedAt,
		slog.Int("file_count", len(preScanFileSet.Files)),
		slog.Int("go_package_target_count", len(goPackageTargets)),
	)

	repoMetadata, err := repositoryidentity.MetadataFor(
		filepath.Base(repoPath),
		repoPath,
		repository.RemoteURL,
	)
	if err != nil {
		return RepositorySnapshot{}, fmt.Errorf("repository metadata for %q: %w", repoPath, err)
	}
	parseStartedAt := time.Now()
	parsePartitionCount := 1
	if s.ParseWorkers > 1 {
		parsePartitionCount = len(buildParseSubtreePartitions(repoPath, parserFileSet.Files, s.ParseWorkers))
	}
	shapeFiles, parsedFiles, languageParseSummary, err := s.buildParsedRepositoryFiles(
		ctx,
		repoPath,
		parserFileSet,
		engine,
		commitSHA,
		repository.IsDependency,
		goPackageTargets,
		repoMetadata.ID,
	)
	if err != nil {
		return RepositorySnapshot{}, fmt.Errorf("build parsed repository files for %q: %w", repoPath, err)
	}
	s.recordSnapshotStage(
		ctx, repoPath, telemetry.SnapshotStageParse, parseStartedAt,
		slog.Int("file_count", len(parserFileSet.Files)),
		slog.Int("parsed_file_count", len(parsedFiles)),
		slog.Int("skipped_file_count", len(parserFileSet.Files)-len(parsedFiles)),
		slog.Int("parse_workers", effectiveSnapshotParseWorkers(s.ParseWorkers)),
		slog.Int("parse_partition_count", parsePartitionCount),
		slog.Any("language_parse_summary", languageParseSummary),
	)
	if len(deriveEligiblePreScanFiles) > 0 {
		// Derive-from-parse only covers files the parse stage actually parsed;
		// a derive-eligible file that parse-skips but would have pre-scanned
		// cleanly is silently omitted rather than aborting the snapshot — that
		// is leniency (completed snapshot), not wrong truth, and both stages run
		// tree-sitter over the same bytes so they fail together in practice.
		mergeParsedFilesIntoDerivedImportsMap(importsMap, parsedFiles)
		// Only the derived path needs the final sort; the pure-legacy path's
		// PreScanRepositoryPathsWithWorkers already sorted each path list.
		finalizeDerivedPreScanImportsMap(importsMap)
	}
	snapshot.ImportsMap = importsMap

	materializeStartedAt := time.Now()
	materialization, err := shape.Materialize(shape.Input{
		RepoID:       repoMetadata.ID,
		SourceSystem: "git",
		Files:        shapeFiles,
	})
	if err != nil {
		return RepositorySnapshot{}, fmt.Errorf("materialize repository content: %w", err)
	}
	s.recordSnapshotStage(
		ctx, repoPath, telemetry.SnapshotStageMaterialize, materializeStartedAt,
		slog.Int("parsed_file_count", len(parsedFiles)),
		slog.Int("content_file_count", len(materialization.Records)),
		slog.Int("content_entity_count", len(materialization.Entities)),
		// file_entity_cap_hit_count: number of files skipped for entity
		// materialization because the projected entity count exceeded
		// MaxFileEntityCount. These are typically minified JS bundles or generated
		// source files with pathological symbol density. When non-zero, the writer
		// retracts any stale content_entities for those paths via PurgeEntities.
		slog.Int("file_entity_cap_hit_count", materialization.FileEntityCapHits),
	)

	valueFlowStartedAt := time.Now()
	// Build lookup structures once per snapshot — the five value-flow
	// builders previously each rebuilt identical structures from the same
	// materialization.Entities slice (#4879).
	entityUIDLookup := buildEntityUIDLookup(materialization.Entities)
	functionUIDResolver := newFunctionUIDResolver(materialization.Entities)

	annotateParsedFilesWithEntityIDs(repoPath, parsedFiles, entityUIDLookup)
	snapshot.TaintEvidence = buildTaintEvidence(repoPath, parsedFiles, entityUIDLookup)
	snapshot.InterprocTaintEvidence = buildInterprocTaintEvidence(repoPath, parsedFiles, functionUIDResolver)
	snapshot.FunctionSummaries = buildFunctionSummaries(repoPath, parsedFiles, functionUIDResolver)
	snapshot.FunctionSources = buildFunctionSources(parsedFiles)
	snapshot.DataflowFunctions = buildDataflowFunctions(repoPath, parsedFiles, entityUIDLookup)
	snapshot.DataflowCatalogVersions = buildDataflowCatalogVersions(parsedFiles)
	// Record that the value-flow gate ran so a per-generation marker fact is
	// emitted even when no findings were produced, letting the reducer retract
	// stale evidence when a generation's finding set goes empty (#2919).
	snapshot.DataflowScanned = s.EmitDataflow
	s.recordSnapshotStage(
		ctx, repoPath, telemetry.SnapshotStageValueFlowEvidence, valueFlowStartedAt,
		slog.Int("parsed_file_count", len(parsedFiles)),
		slog.Int("taint_evidence_count", len(snapshot.TaintEvidence)),
		slog.Int("interproc_taint_evidence_count", len(snapshot.InterprocTaintEvidence)),
		slog.Int("function_summary_count", len(snapshot.FunctionSummaries)),
		slog.Int("function_source_count", len(snapshot.FunctionSources)),
		slog.Int("dataflow_function_count", len(snapshot.DataflowFunctions)),
		slog.Bool("dataflow_scanned", s.EmitDataflow),
	)
	snapshot.FileData = parsedFiles
	// snapshot.ContentFileMetas already carries the .gitmodules candidate
	// meta set and the codeowners candidate metas set earlier (before the
	// zero-files early return); append the materialized content metas rather
	// than overwrite them.
	snapshot.ContentFileMetas = append(snapshot.ContentFileMetas, materializationRecordsToMetas(materialization.Records)...)
	snapshot.DocumentationFileMetas = documentationFileMetasForPaths(repoPath, discovery.FilePaths(documentationFiles), commitSHA)
	snapshot.ContentEntities = materializationEntitiesToSnapshots(materialization.Entities, s.now())
	snapshot.DiscoveryAdvisory = buildDiscoveryAdvisoryReport(
		repoPath,
		s.now(),
		discoveryStats,
		fileSet.FilePaths(),
		snapshot.ContentFileMetas,
		snapshot.ContentEntities,
		commitSHA,
	)

	// Release body references — bodies are no longer needed in the snapshot.
	// streamFacts will re-read each file from disk when building content facts.
	// The OS page cache keeps file contents warm, so re-reads are nearly free.
	//nolint:ineffassign // intentional: drop references so GC can collect bodies
	shapeFiles = nil
	//nolint:ineffassign
	materialization = content.Materialization{}

	return snapshot, nil
}

// recordSnapshotStage emits per-stage repository snapshot timing as a structured
// log, a metric histogram, and a trace span so operators can distinguish
// discovery, pre-scan, parse, materialization, and value-flow evidence
// bottlenecks before changing concurrency or graph-write tuning.
//
// The stage argument MUST be a bounded telemetry.SnapshotStage* value. The span
// is back-dated to startedAt so the trace reflects the real stage window even
// though it is recorded after the stage completes; this keeps the call sites a
// single post-stage statement rather than a control-flow rewrite.
//
// recordSnapshotStage captures the stage end time itself (time.Now()), so the
// stage window silently grows to include any variadic attrs expression the
// caller evaluates before the call — Go evaluates all call arguments before
// invoking the function. Any call site whose attrs construction does
// non-trivial work (a per-file summary loop, a metric-recording pass) MUST
// capture its own endedAt first and call recordSnapshotStageAt instead, or the
// stage-duration metric double-counts that post-processing cost (#4767).
func (s NativeRepositorySnapshotter) recordSnapshotStage(
	ctx context.Context,
	repoPath string,
	stage string,
	startedAt time.Time,
	attrs ...any,
) {
	s.recordSnapshotStageAt(ctx, repoPath, stage, startedAt, time.Now(), attrs...)
}

// recordSnapshotStageAt is recordSnapshotStage with an explicit, already-captured
// end time. Use this when building the attrs for a stage does measurable work
// (e.g. a per-file telemetry summary loop) so that work is not folded into the
// reported stage duration.
func (s NativeRepositorySnapshotter) recordSnapshotStageAt(
	ctx context.Context,
	repoPath string,
	stage string,
	startedAt time.Time,
	endedAt time.Time,
	attrs ...any,
) {
	duration := endedAt.Sub(startedAt)

	if s.Instruments != nil {
		s.Instruments.CollectorSnapshotStageDuration.Record(
			ctx, duration.Seconds(),
			metric.WithAttributes(
				telemetry.AttrCollectorKind("git"),
				telemetry.AttrStage(stage),
			),
		)
	}

	if s.Tracer != nil {
		_, span := s.Tracer.Start(
			ctx, telemetry.SpanCollectorSnapshotStage,
			trace.WithTimestamp(startedAt),
			trace.WithAttributes(
				telemetry.AttrCollectorKind("git"),
				telemetry.AttrStage(stage),
			),
		)
		span.End(trace.WithTimestamp(endedAt))
	}

	if s.Logger != nil {
		logAttrs := []any{
			log.CollectorKind("git"),
			log.RepoPath(repoPath),
			slog.String("stage", stage),
			slog.Float64("duration_seconds", duration.Seconds()),
		}
		logAttrs = append(logAttrs, attrs...)
		s.Logger.InfoContext(ctx, "collector snapshot stage completed", logAttrs...)
	}
}
