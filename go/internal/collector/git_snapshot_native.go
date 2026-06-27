// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
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

var snapshotEntityBuckets = []struct {
	bucket string
	label  string
}{
	{bucket: "functions", label: "Function"},
	{bucket: "classes", label: "Class"},
	{bucket: "modules", label: "Module"},
	{bucket: "variables", label: "Variable"},
	{bucket: "type_annotations", label: "TypeAnnotation"},
	{bucket: "traits", label: "Trait"},
	{bucket: "interfaces", label: "Interface"},
	{bucket: "macros", label: "Macro"},
	{bucket: "structs", label: "Struct"},
	{bucket: "enums", label: "Enum"},
	{bucket: "protocols", label: "Protocol"},
	{bucket: "unions", label: "Union"},
	{bucket: "typedefs", label: "Typedef"},
	{bucket: "type_aliases", label: "TypeAlias"},
	{bucket: "annotations", label: "Annotation"},
	{bucket: "records", label: "Record"},
	{bucket: "properties", label: "Property"},
	{bucket: "components", label: "Component"},
	{bucket: "k8s_resources", label: "K8sResource"},
	{bucket: "argocd_applications", label: "ArgoCDApplication"},
	{bucket: "argocd_applicationsets", label: "ArgoCDApplicationSet"},
	{bucket: "crossplane_xrds", label: "CrossplaneXRD"},
	{bucket: "crossplane_compositions", label: "CrossplaneComposition"},
	{bucket: "crossplane_claims", label: "CrossplaneClaim"},
	{bucket: "kustomize_overlays", label: "KustomizeOverlay"},
	{bucket: "helm_charts", label: "HelmChart"},
	{bucket: "helm_values", label: "HelmValues"},
	{bucket: "terraform_resources", label: "TerraformResource"},
	{bucket: "terraform_variables", label: "TerraformVariable"},
	{bucket: "terraform_outputs", label: "TerraformOutput"},
	{bucket: "terraform_modules", label: "TerraformModule"},
	{bucket: "terraform_data_sources", label: "TerraformDataSource"},
	{bucket: "terraform_providers", label: "TerraformProvider"},
	{bucket: "terraform_locals", label: "TerraformLocal"},
	{bucket: "terraform_backends", label: "TerraformBackend"},
	{bucket: "terraform_imports", label: "TerraformImport"},
	{bucket: "terraform_moved_blocks", label: "TerraformMovedBlock"},
	{bucket: "terraform_removed_blocks", label: "TerraformRemovedBlock"},
	{bucket: "terraform_checks", label: "TerraformCheck"},
	{bucket: "terraform_lock_providers", label: "TerraformLockProvider"},
	{bucket: "terragrunt_configs", label: "TerragruntConfig"},
	{bucket: "terragrunt_dependencies", label: "TerragruntDependency"},
	{bucket: "terragrunt_locals", label: "TerragruntLocal"},
	{bucket: "terragrunt_inputs", label: "TerragruntInput"},
	{bucket: "cloudformation_resources", label: "CloudFormationResource"},
	{bucket: "cloudformation_parameters", label: "CloudFormationParameter"},
	{bucket: "cloudformation_outputs", label: "CloudFormationOutput"},
	{bucket: "atlantis_projects", label: "AtlantisProject"},
	{bucket: "atlantis_workflows", label: "AtlantisWorkflow"},
	{bucket: "gitlab_pipelines", label: "GitlabPipeline"},
	{bucket: "gitlab_jobs", label: "GitlabJob"},
	{bucket: "sql_tables", label: "SqlTable"},
	{bucket: "sql_columns", label: "SqlColumn"},
	{bucket: "sql_views", label: "SqlView"},
	{bucket: "sql_functions", label: "SqlFunction"},
	{bucket: "sql_triggers", label: "SqlTrigger"},
	{bucket: "sql_indexes", label: "SqlIndex"},
	{bucket: "analytics_models", label: "AnalyticsModel"},
	{bucket: "data_assets", label: "DataAsset"},
	{bucket: "data_columns", label: "DataColumn"},
	{bucket: "query_executions", label: "QueryExecution"},
	{bucket: "dashboard_assets", label: "DashboardAsset"},
	{bucket: "data_quality_checks", label: "DataQualityCheck"},
	{bucket: "data_owners", label: "DataOwner"},
	{bucket: "data_contracts", label: "DataContract"},
	{bucket: "impl_blocks", label: "ImplBlock"},
	{bucket: "pagerduty_declarations", label: "PagerDutyDeclaration"},
}

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
	parserFiles, documentationFiles := partitionNativeSnapshotFiles(fileSet.Files, registry)
	fullParserFiles, _ := partitionNativeSnapshotFiles(fullFileSet.Files, registry)
	parserFileSet := fileSet
	parserFileSet.Files = parserFiles
	preScanFileSet := parserFileSet
	preScanFileSet.Files = sortUniquePathStrings(append(
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
	commitSHA := gitCommitSHA(ctx, repoPath)
	snapshot.HeadCommitSHA = commitSHA
	if len(fileSet.Files) == 0 {
		snapshot.DiscoveryAdvisory = buildDiscoveryAdvisoryReport(
			repoPath,
			s.now(),
			discoveryStats,
			fileSet.Files,
			nil,
			nil,
			commitSHA,
		)
		return snapshot, nil
	}

	preScanStartedAt := time.Now()
	importsMap, err := engine.PreScanRepositoryPathsWithWorkers(
		repoPath,
		preScanFileSet.Files,
		effectiveSnapshotParseWorkers(s.ParseWorkers),
	)
	if err != nil {
		return RepositorySnapshot{}, fmt.Errorf("pre-scan repository imports for %q: %w", repoPath, err)
	}
	snapshot.ImportsMap = importsMap
	s.recordSnapshotStage(
		ctx, repoPath, telemetry.SnapshotStagePreScan, preScanStartedAt,
		slog.Int("file_count", len(preScanFileSet.Files)),
		slog.Int("import_symbol_count", len(importsMap)),
		slog.Int("pre_scan_workers", effectiveSnapshotParseWorkers(s.ParseWorkers)),
	)
	goPackageSemanticPreScanStartedAt := time.Now()
	goPackageTargets, err := engine.PreScanGoPackageSemanticRoots(repoPath, preScanFileSet.Files)
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
	annotateParsedFilesWithEntityIDs(repoPath, parsedFiles, materialization.Entities)
	snapshot.TaintEvidence = buildTaintEvidence(repoPath, parsedFiles, materialization.Entities)
	snapshot.InterprocTaintEvidence = buildInterprocTaintEvidence(repoPath, parsedFiles, materialization.Entities)
	snapshot.FunctionSummaries = buildFunctionSummaries(repoPath, parsedFiles, materialization.Entities)
	snapshot.FunctionSources = buildFunctionSources(parsedFiles)
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
		slog.Bool("dataflow_scanned", s.EmitDataflow),
	)
	snapshot.FileData = parsedFiles
	snapshot.ContentFileMetas = materializationRecordsToMetas(materialization.Records)
	snapshot.DocumentationFileMetas = documentationFileMetasForPaths(repoPath, documentationFiles, commitSHA)
	snapshot.ContentEntities = materializationEntitiesToSnapshots(materialization.Entities, s.now())
	snapshot.DiscoveryAdvisory = buildDiscoveryAdvisoryReport(
		repoPath,
		s.now(),
		discoveryStats,
		fileSet.Files,
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
func (s NativeRepositorySnapshotter) recordSnapshotStage(
	ctx context.Context,
	repoPath string,
	stage string,
	startedAt time.Time,
	attrs ...any,
) {
	endedAt := time.Now()
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

// effectiveSnapshotParseWorkers reports the actual parser worker cardinality
// when the zero-value configuration falls back to the sequential parser path.
func effectiveSnapshotParseWorkers(configured int) int {
	if configured <= 1 {
		return 1
	}
	return configured
}

func (s NativeRepositorySnapshotter) engine() (*parser.Engine, error) {
	if s.Engine != nil {
		return s.Engine, nil
	}
	return parser.DefaultEngine()
}

func (s NativeRepositorySnapshotter) registry() parser.Registry {
	if len(s.Registry.ParserKeys()) > 0 {
		return s.Registry
	}
	return parser.DefaultRegistry()
}
