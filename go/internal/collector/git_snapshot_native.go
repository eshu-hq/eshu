package collector

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
}

// NativeRepositorySnapshotter builds repository snapshots without Python bridge code.
type NativeRepositorySnapshotter struct {
	Engine           *parser.Engine
	Registry         parser.Registry
	DiscoveryOptions discovery.Options
	SCIP             SnapshotSCIPConfig
	Now              func() time.Time
	ParseWorkers     int
	Tracer           trace.Tracer
	Instruments      *telemetry.Instruments
	Logger           *slog.Logger
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
	fileSet, discoveryStats, err := resolveNativeSnapshotFileSet(repoPath, registry, s.discoveryOptions())
	if len(repository.FileTargets) > 0 {
		fileSet, err = resolveNativeSnapshotFileSetForTargets(repoPath, repository.FileTargets, registry)
	}
	if err != nil {
		return RepositorySnapshot{}, err
	}
	var tfstateCandidates []TerraformStateCandidate
	fileSet.Files, tfstateCandidates = extractTerraformStateCandidates(repoPath, fileSet.Files)
	logTerraformStateCandidateDiscovery(ctx, s, repoPath, len(tfstateCandidates))
	s.logDiscoveryStats(ctx, repoPath, discoveryStats)
	s.logSnapshotStageTiming(ctx, repoPath, "discovery", discoveryStartedAt,
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
	}
	commitSHA := gitCommitSHA(ctx, repoPath)
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
		fileSet.Files,
		effectiveSnapshotParseWorkers(s.ParseWorkers),
	)
	if err != nil {
		return RepositorySnapshot{}, fmt.Errorf("pre-scan repository imports for %q: %w", repoPath, err)
	}
	snapshot.ImportsMap = importsMap
	s.logSnapshotStageTiming(ctx, repoPath, "pre_scan", preScanStartedAt,
		slog.Int("file_count", len(fileSet.Files)),
		slog.Int("import_symbol_count", len(importsMap)),
		slog.Int("pre_scan_workers", effectiveSnapshotParseWorkers(s.ParseWorkers)),
	)
	goPackageTargets, err := engine.PreScanGoPackageSemanticRoots(repoPath, fileSet.Files)
	if err != nil {
		return RepositorySnapshot{}, fmt.Errorf("pre-scan go package interface params for %q: %w", repoPath, err)
	}

	repoMetadata, err := repositoryidentity.MetadataFor(
		filepath.Base(repoPath),
		repoPath,
		repository.RemoteURL,
	)
	if err != nil {
		return RepositorySnapshot{}, fmt.Errorf("repository metadata for %q: %w", repoPath, err)
	}
	parseStartedAt := time.Now()
	shapeFiles, parsedFiles, err := s.buildParsedRepositoryFiles(
		ctx,
		repoPath,
		fileSet,
		engine,
		commitSHA,
		repository.IsDependency,
		goPackageTargets,
	)
	if err != nil {
		return RepositorySnapshot{}, fmt.Errorf("build parsed repository files for %q: %w", repoPath, err)
	}
	s.logSnapshotStageTiming(ctx, repoPath, "parse", parseStartedAt,
		slog.Int("file_count", len(fileSet.Files)),
		slog.Int("parsed_file_count", len(parsedFiles)),
		slog.Int("skipped_file_count", len(fileSet.Files)-len(parsedFiles)),
		slog.Int("parse_workers", effectiveSnapshotParseWorkers(s.ParseWorkers)),
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
	s.logSnapshotStageTiming(ctx, repoPath, "materialize", materializeStartedAt,
		slog.Int("parsed_file_count", len(parsedFiles)),
		slog.Int("content_file_count", len(materialization.Records)),
		slog.Int("content_entity_count", len(materialization.Entities)),
	)

	annotateParsedFilesWithEntityIDs(repoPath, parsedFiles, materialization.Entities)
	snapshot.FileData = parsedFiles
	snapshot.ContentFileMetas = materializationRecordsToMetas(materialization.Records)
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

// logSnapshotStageTiming emits coarse repository snapshot phase timings so
// operators can distinguish parser, materialization, and commit bottlenecks
// before changing concurrency or graph-write tuning.
func (s NativeRepositorySnapshotter) logSnapshotStageTiming(
	ctx context.Context,
	repoPath string,
	stage string,
	startedAt time.Time,
	attrs ...any,
) {
	if s.Logger == nil {
		return
	}
	logAttrs := []any{
		slog.String("collector_kind", "git"),
		slog.String("repo_path", repoPath),
		slog.String("stage", stage),
		slog.Float64("duration_seconds", time.Since(startedAt).Seconds()),
	}
	logAttrs = append(logAttrs, attrs...)
	s.Logger.InfoContext(ctx, "collector snapshot stage completed", logAttrs...)
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

// defaultIgnoredDirs lists directories that are always pruned during discovery.
// This matches the Python-era default exclusion list plus additional coverage
// for every language the parser registry supports.
var defaultIgnoredDirs = []string{
	// VCS
	".git",
	".svn",
	".hg",
	// Eshu repo-local configuration is operator metadata, not source input.
	".eshu",
	// Infrastructure / IaC caches
	".terraform",
	".terragrunt-cache",
	".tox",
	".mypy_cache",
	".pytest_cache",
	".aws-sam",
	"cdk.out",
	".serverless",
	// JavaScript / TypeScript
	"node_modules",
	"bower_components",
	"jspm_packages",
	// Yarn Berry stores package-manager bundles and caches under .yarn.
	// These generated artifacts can dwarf real application source.
	".yarn",
	".next",
	".nuxt",
	// Python
	"site-packages",
	"dist-packages",
	"__pypackages__",
	"__pycache__",
	".venv",
	"venv",
	".eggs",
	// PHP
	"vendor",
	"wp-admin",
	"wp-includes",
	// Go
	// (vendor already listed under PHP)
	// Ruby
	"bundle",
	// Elixir
	"deps",
	"_build",
	// Swift ecosystem
	"Pods",
	".build",
	"Carthage",
	// Java / Kotlin / Scala / Groovy
	".gradle",
	".m2",
	".ivy2",
	// Rust
	// (target already listed below under build output)
	// Haskell
	".stack-work",
	".cabal-sandbox",
	"dist-newstyle",
	// Dart
	".dart_tool",
	".pub-cache",
	// Perl
	"blib",
	"local",
	// C / C#
	"packages",
	"obj",
	"bin",
	// Ansible
	".ansible",
	"ansible_collections",
	// Jenkins
	".jenkins",
	// Common build and distribution output
	"dist",
	"build",
	"target",
	"out",
	// Coverage and test output
	"coverage",
	".nyc_output",
	"htmlcov",
}

// defaultIgnoredExtensions lists file suffixes that are always skipped during
// discovery. These cover log/output artifacts, minified/bundled assets, and
// other non-source files that should never be parsed.
var defaultIgnoredExtensions = []string{
	// Logs and output
	".log",
	".out",
	// Minified and bundled assets
	".min.js",
	".min.mjs",
	".min.css",
	".bundle.js",
	".chunk.js",
	".min.map",
	// Source maps
	".map",
	// Yarn Berry Plug'n'Play loader files are generated dependency metadata.
	".pnp.cjs",
	".pnp.loader.mjs",
	// Compiled / binary artifacts commonly checked in
	".pyc",
	".pyo",
	".class",
	".dll",
	".so",
	".dylib",
	".exe",
	".o",
	".a",
	".wasm",
}

func (s NativeRepositorySnapshotter) discoveryOptions() discovery.Options {
	opts := defaultNativeDiscoveryOptions()
	opts.IgnoredDirs = append(opts.IgnoredDirs, s.DiscoveryOptions.IgnoredDirs...)
	opts.IgnoredExtensions = append(opts.IgnoredExtensions, s.DiscoveryOptions.IgnoredExtensions...)
	opts.IgnoreHidden = s.DiscoveryOptions.IgnoreHidden
	opts.PreservedHiddenPrefixes = append(opts.PreservedHiddenPrefixes, s.DiscoveryOptions.PreservedHiddenPrefixes...)
	opts.HonorGitignore = true
	opts.HonorEshuIgnore = true
	opts.IgnoredPathGlobs = append(opts.IgnoredPathGlobs, s.DiscoveryOptions.IgnoredPathGlobs...)
	opts.PreservedPathGlobs = append(opts.PreservedPathGlobs, s.DiscoveryOptions.PreservedPathGlobs...)
	return opts
}

func defaultNativeDiscoveryOptions() discovery.Options {
	return discovery.Options{
		IgnoredDirs:       defaultIgnoredDirs,
		IgnoredExtensions: defaultIgnoredExtensions,
		HonorGitignore:    true,
		HonorEshuIgnore:   true,
	}
}

func (s NativeRepositorySnapshotter) logDiscoveryStats(ctx context.Context, repoPath string, stats discovery.DiscoveryStats) {
	logger := s.Logger
	if logger == nil {
		logger = slog.Default()
	}

	attrs := []any{
		slog.String("repo_path", filepath.Base(repoPath)),
		slog.Int("dirs_skipped_total", stats.TotalDirsSkipped()),
		slog.Int("files_skipped_total", stats.TotalFilesSkipped()),
	}

	// Log per-directory-name breakdown for operator tuning visibility.
	for name, count := range stats.DirsSkippedByName {
		attrs = append(attrs, slog.Int("dirs_skipped."+name, count))
	}
	for ext, count := range stats.FilesSkippedByExtension {
		attrs = append(attrs, slog.Int("files_skipped.ext"+ext, count))
	}
	for reason, count := range stats.FilesSkippedByContent {
		attrs = append(attrs, slog.Int("files_skipped.content."+reason, count))
	}
	for reason, count := range stats.DirsSkippedByUser {
		attrs = append(attrs, slog.Int("dirs_skipped.user."+reason, count))
	}
	for reason, count := range stats.FilesSkippedByUser {
		attrs = append(attrs, slog.Int("files_skipped.user."+reason, count))
	}
	if stats.FilesSkippedHidden > 0 {
		attrs = append(attrs, slog.Int("files_skipped.hidden", stats.FilesSkippedHidden))
	}
	if stats.FilesSkippedGitignore > 0 {
		attrs = append(attrs, slog.Int("files_skipped.gitignore", stats.FilesSkippedGitignore))
	}
	if stats.FilesSkippedEshuIgnore > 0 {
		attrs = append(attrs, slog.Int("files_skipped.eshuignore", stats.FilesSkippedEshuIgnore))
	}

	logger.InfoContext(ctx, "discovery stats", attrs...)
}

func (s NativeRepositorySnapshotter) recordDiscoveryMetrics(ctx context.Context, stats discovery.DiscoveryStats) {
	if s.Instruments == nil {
		return
	}

	for name, count := range stats.DirsSkippedByName {
		s.Instruments.DiscoveryDirsSkipped.Add(ctx, int64(count),
			metric.WithAttributes(telemetry.AttrSkipReason(name)),
		)
	}
	for ext, count := range stats.FilesSkippedByExtension {
		s.Instruments.DiscoveryFilesSkipped.Add(ctx, int64(count),
			metric.WithAttributes(telemetry.AttrSkipReason("ext:"+ext)),
		)
	}
	for reason, count := range stats.FilesSkippedByContent {
		s.Instruments.DiscoveryFilesSkipped.Add(ctx, int64(count),
			metric.WithAttributes(telemetry.AttrSkipReason("content:"+reason)),
		)
	}
	for reason, count := range stats.DirsSkippedByUser {
		s.Instruments.DiscoveryDirsSkipped.Add(ctx, int64(count),
			metric.WithAttributes(telemetry.AttrSkipReason("user:"+reason)),
		)
	}
	for reason, count := range stats.FilesSkippedByUser {
		s.Instruments.DiscoveryFilesSkipped.Add(ctx, int64(count),
			metric.WithAttributes(telemetry.AttrSkipReason("user:"+reason)),
		)
	}
	if stats.FilesSkippedHidden > 0 {
		s.Instruments.DiscoveryFilesSkipped.Add(ctx, int64(stats.FilesSkippedHidden),
			metric.WithAttributes(telemetry.AttrSkipReason("hidden")),
		)
	}
	if stats.FilesSkippedGitignore > 0 {
		s.Instruments.DiscoveryFilesSkipped.Add(ctx, int64(stats.FilesSkippedGitignore),
			metric.WithAttributes(telemetry.AttrSkipReason("gitignore")),
		)
	}
	if stats.FilesSkippedEshuIgnore > 0 {
		s.Instruments.DiscoveryFilesSkipped.Add(ctx, int64(stats.FilesSkippedEshuIgnore),
			metric.WithAttributes(telemetry.AttrSkipReason("eshuignore")),
		)
	}
}

func (s NativeRepositorySnapshotter) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func resolveNativeSnapshotFileSet(
	repoPath string,
	registry parser.Registry,
	opts discovery.Options,
) (discovery.RepoFileSet, discovery.DiscoveryStats, error) {
	opts, err := discoveryOptionsWithRepoDiscoveryConfig(repoPath, opts)
	if err != nil {
		return discovery.RepoFileSet{}, discovery.DiscoveryStats{}, err
	}
	stats, fileSets, err := discovery.ResolveRepositoryFileSetsWithStats(
		repoPath,
		func(path string) bool {
			if isTerraformStateCandidateName(filepath.Base(path)) {
				return true
			}
			_, ok := registry.LookupByPath(path)
			return ok
		},
		opts,
	)
	if err != nil {
		return discovery.RepoFileSet{}, stats, fmt.Errorf("resolve repository file sets: %w", err)
	}
	for _, fileSet := range fileSets {
		if fileSet.RepoRoot == repoPath {
			fileSet.Files = filterGeneratedNativeSnapshotFiles(fileSet.Files, &stats)
			return fileSet, stats, nil
		}
	}
	return discovery.RepoFileSet{RepoRoot: repoPath}, stats, nil
}

const generatedJavaScriptBundleMinBytes = 256 * 1024
const vendoredBrowserLibraryPrefixBytes = 16 * 1024

func filterGeneratedNativeSnapshotFiles(files []string, stats *discovery.DiscoveryStats) []string {
	filtered := files[:0]
	for _, path := range files {
		if reason, ok := generatedNativeSnapshotSkipReason(path); ok {
			if stats.FilesSkippedByContent == nil {
				stats.FilesSkippedByContent = make(map[string]int)
			}
			stats.FilesSkippedByContent[reason]++
			continue
		}
		filtered = append(filtered, path)
	}
	return filtered
}

func generatedNativeSnapshotSkipReason(path string) (string, bool) {
	if isVendoredZendFrameworkFile(path) {
		return "vendored-zend-framework", true
	}
	if isVendoredBrowserLibraryFile(path) {
		return "vendored-browser-library", true
	}
	if isVendoredFPDFFile(path) {
		return "vendored-fpdf", true
	}
	if isVendoredPEARFile(path) {
		return "vendored-pear", true
	}
	prefix, ok := largeJavaScriptBundlePrefix(path)
	if !ok {
		return "", false
	}
	switch {
	case isWebpackBootstrapPrefix(prefix):
		return "generated-webpack", true
	case isRollupBootstrapPrefix(prefix):
		return "generated-rollup", true
	case isESBuildBootstrapPrefix(prefix):
		return "generated-esbuild", true
	case isParcelBootstrapPrefix(prefix):
		return "generated-parcel", true
	}
	return "", false
}

func largeJavaScriptBundlePrefix(path string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".js" && ext != ".cjs" && ext != ".mjs" {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() < generatedJavaScriptBundleMinBytes {
		return "", false
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer func() { _ = file.Close() }()

	buf := make([]byte, 8192)
	n, err := file.Read(buf)
	if err != nil && n == 0 {
		return "", false
	}
	return string(buf[:n]), true
}

func isWebpackBootstrapPrefix(prefix string) bool {
	return strings.Contains(prefix, "webpackBootstrap") &&
		strings.Contains(prefix, "/******/") &&
		(strings.Contains(prefix, "installedModules") ||
			(strings.Contains(prefix, "__webpack_modules__") &&
				strings.Contains(prefix, "__webpack_module_cache__") &&
				strings.Contains(prefix, "function __webpack_require__")))
}

func isRollupBootstrapPrefix(prefix string) bool {
	return strings.Contains(prefix, "var commonjsGlobal = typeof globalThis") &&
		strings.Contains(prefix, "function getDefaultExportFromCjs") &&
		strings.Contains(prefix, "function getAugmentedNamespace") &&
		strings.Contains(prefix, "Object.defineProperty(a, '__esModule'")
}

func isESBuildBootstrapPrefix(prefix string) bool {
	return strings.Contains(prefix, "var __defProp = Object.defineProperty") &&
		strings.Contains(prefix, "var __getOwnPropNames = Object.getOwnPropertyNames") &&
		strings.Contains(prefix, "var __commonJS =") &&
		strings.Contains(prefix, "var __copyProps =") &&
		(strings.Contains(prefix, "var __toESM =") || strings.Contains(prefix, "var __toCommonJS ="))
}

func isParcelBootstrapPrefix(prefix string) bool {
	return strings.Contains(prefix, "$parcel$global") &&
		strings.Contains(prefix, "parcelRequire") &&
		strings.Contains(prefix, "newRequire.isParcelRequire = true") &&
		strings.Contains(prefix, "newRequire.Module = Module")
}

func isVendoredZendFrameworkFile(path string) bool {
	normalized := filepath.ToSlash(path)
	if !strings.Contains(normalized, "/library/Zend/") && !strings.Contains(normalized, "/Zend/") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".php"
}

func isVendoredBrowserLibraryFile(path string) bool {
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".js" && ext != ".mjs" && ext != ".cjs" {
		return false
	}
	name := strings.ToLower(filepath.Base(path))
	switch {
	case name == "jquery.js" || name == "jquery-ui.js":
		return true
	case strings.HasPrefix(name, "jquery.") || strings.HasPrefix(name, "jquery-"):
		return true
	case strings.HasPrefix(name, "galleria") && strings.HasSuffix(name, ".js"):
		return true
	case strings.HasPrefix(name, "shadowbox") && strings.HasSuffix(name, ".js"):
		return true
	case strings.HasPrefix(name, "sizzle") && strings.HasSuffix(name, ".js"):
		return true
	case strings.HasPrefix(name, "swfobject") && strings.HasSuffix(name, ".js"):
		return true
	case strings.Contains("/"+filepath.ToSlash(path)+"/", "/jwplayer/"):
		return true
	default:
		return hasVendoredBrowserLibrarySignature(path)
	}
}

func hasVendoredBrowserLibrarySignature(path string) bool {
	prefix, ok := javascriptFilePrefix(path, vendoredBrowserLibraryPrefixBytes)
	if !ok {
		return false
	}
	normalized := strings.ToLower(prefix)
	switch {
	case strings.Contains(normalized, "jquery foundation") &&
		strings.Contains(normalized, "jquery.org/license"):
		return true
	case strings.Contains(normalized, "bootstrap v") &&
		strings.Contains(normalized, "getbootstrap.com"):
		return true
	case strings.Contains(normalized, "fullcalendar v") &&
		strings.Contains(normalized, "fullcalendar.io"):
		return true
	case strings.Contains(normalized, "fotorama") &&
		strings.Contains(normalized, "fotorama.io/license"):
		return true
	case strings.Contains(normalized, "gmaps.js") &&
		strings.Contains(normalized, "hpneo.github.com/gmaps"):
		return true
	case strings.Contains(normalized, "masonry packaged") &&
		strings.Contains(normalized, "masonry.desandro.com"):
		return true
	case strings.Contains(normalized, "jwplayer.version") ||
		(strings.Contains(normalized, "d.html5.version") &&
			strings.Contains(normalized, "jwplayer")):
		return true
	case strings.Contains(normalized, "filepond") &&
		strings.Contains(normalized, "pqina.nl/filepond"):
		return true
	case strings.Contains(normalized, "prototype javascript framework") &&
		strings.Contains(normalized, "prototypejs.org"):
		return true
	case strings.Contains(normalized, "reveal.js") &&
		strings.Contains(normalized, "lab.hakim.se/reveal-js"):
		return true
	default:
		return false
	}
}

func javascriptFilePrefix(path string, limit int) (string, bool) {
	if limit <= 0 {
		return "", false
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer func() { _ = file.Close() }()

	buf := make([]byte, limit)
	n, err := file.Read(buf)
	if err != nil && n == 0 {
		return "", false
	}
	return string(buf[:n]), true
}

func isVendoredFPDFFile(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	return name == "fpdf.php"
}

func isVendoredPEARFile(path string) bool {
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".php" {
		return false
	}
	normalized := strings.ToLower(filepath.ToSlash(path))
	return strings.Contains(normalized, "/pear/php/")
}

func resolveNativeSnapshotFileSetForTargets(
	repoPath string,
	fileTargets []string,
	registry parser.Registry,
) (discovery.RepoFileSet, error) {
	files := make([]string, 0, len(fileTargets))
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
				continue
			}
		}
		files = append(files, absoluteTarget)
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

func annotateParsedFilesWithEntityIDs(
	repoPath string,
	parsedFiles []map[string]any,
	entities []content.EntityRecord,
) {
	lookup := make(map[string]string, len(entities))
	for _, entity := range entities {
		key := entityLookupKey(entity.Path, entity.EntityType, entity.EntityName, entity.StartLine)
		lookup[key] = entity.EntityID
	}

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
				if entityID, ok := lookup[key]; ok {
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
	command := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "HEAD")
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func digestForBody(body string) string {
	sum := sha1.Sum([]byte(body))
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
