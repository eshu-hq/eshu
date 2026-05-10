package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// Engine owns native parser dispatch.
type Engine struct {
	registry Registry
	runtime  *Runtime
}

// DefaultEngine constructs the native parser engine for the built-in registry.
func DefaultEngine() (*Engine, error) {
	return NewEngine(DefaultRegistry(), NewRuntime())
}

// NewEngine constructs one parser engine instance.
func NewEngine(registry Registry, runtime *Runtime) (*Engine, error) {
	if runtime == nil {
		return nil, fmt.Errorf("parser runtime is required")
	}
	return &Engine{
		registry: registry,
		runtime:  runtime,
	}, nil
}

// ParsePath parses one file through the built-in engine contract.
func (e *Engine) ParsePath(
	repoRoot string,
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	resolvedRepoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve repo root %q: %w", repoRoot, err)
	}
	resolvedPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path %q: %w", path, err)
	}

	definition, ok := e.registry.LookupByPath(resolvedPath)
	if !ok {
		return nil, fmt.Errorf("no parser registered for %q", resolvedPath)
	}

	payload, err := e.parseDefinition(resolvedRepoRoot, definition, resolvedPath, isDependency, options)
	if err != nil {
		return nil, err
	}
	if source, readErr := readSource(resolvedPath); readErr == nil {
		metadata := inferContentMetadata(resolvedPath, string(source))
		if strings.TrimSpace(metadata.ArtifactType) != "" {
			payload["artifact_type"] = metadata.ArtifactType
		}
		if strings.TrimSpace(metadata.TemplateDialect) != "" {
			payload["template_dialect"] = metadata.TemplateDialect
		}
		payload["iac_relevant"] = metadata.IACRelevant
	}
	payload["repo_path"] = resolvedRepoRoot
	return payload, nil
}

// PreScanPaths returns the import-map contract used by the collector prescan path.
func (e *Engine) PreScanPaths(paths []string) (map[string][]string, error) {
	return e.preScanPathsWithRoot("", paths, 1)
}

// PreScanRepositoryPaths returns the import-map contract used by collectors
// that already know the repository root and want prescan logic bounded to it.
func (e *Engine) PreScanRepositoryPaths(repoRoot string, paths []string) (map[string][]string, error) {
	return e.preScanPathsWithRoot(repoRoot, paths, 1)
}

// PreScanRepositoryPathsWithWorkers returns repository-bounded pre-scan
// results using a bounded worker pool. Results are merged after workers finish
// so callers keep deterministic output ordering while large repositories avoid
// a single serial parser bottleneck before the normal parse stage.
func (e *Engine) PreScanRepositoryPathsWithWorkers(repoRoot string, paths []string, workers int) (map[string][]string, error) {
	return e.preScanPathsWithRoot(repoRoot, paths, workers)
}

func (e *Engine) preScanPathsWithRoot(repoRoot string, paths []string, workers int) (map[string][]string, error) {
	resolvedRepoRoot := strings.TrimSpace(repoRoot)
	if resolvedRepoRoot != "" {
		absRepoRoot, err := filepath.Abs(resolvedRepoRoot)
		if err != nil {
			return nil, fmt.Errorf("resolve prescan repo root %q: %w", repoRoot, err)
		}
		resolvedRepoRoot = absRepoRoot
	}

	if workers <= 1 || len(paths) <= 1 {
		return e.preScanPathsSequential(resolvedRepoRoot, paths)
	}
	return e.preScanPathsConcurrent(resolvedRepoRoot, paths, workers)
}

// preScanPathsSequential preserves the historical first-error behavior for
// callers that do not opt into the worker-aware repository pre-scan path.
func (e *Engine) preScanPathsSequential(resolvedRepoRoot string, paths []string) (map[string][]string, error) {
	results := make(map[string][]string)
	for _, rawPath := range paths {
		scanned, err := e.preScanOnePath(resolvedRepoRoot, rawPath)
		if err != nil {
			return nil, err
		}
		mergePreScanPathResult(results, scanned)
	}
	sortPreScanResults(results)
	return results, nil
}

// preScanPathResult carries one file's resolved path and discovered names so
// concurrent workers can merge results after sorting by original input order.
type preScanPathResult struct {
	index int
	path  string
	names []string
	err   error
}

// preScanPathsConcurrent scans files in parallel, then returns the first input
// order error and sorted per-name path lists to keep output deterministic.
func (e *Engine) preScanPathsConcurrent(resolvedRepoRoot string, paths []string, workers int) (map[string][]string, error) {
	type preScanJob struct {
		index int
		path  string
	}

	jobs := make(chan preScanJob, len(paths))
	results := make(chan preScanPathResult, len(paths))
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				scanned, err := e.preScanOnePath(resolvedRepoRoot, job.path)
				scanned.index = job.index
				scanned.err = err
				results <- scanned
			}
		}()
	}

	for i, path := range paths {
		jobs <- preScanJob{index: i, path: path}
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	resultSlice := make([]preScanPathResult, 0, len(paths))
	for result := range results {
		resultSlice = append(resultSlice, result)
	}
	slices.SortFunc(resultSlice, func(left, right preScanPathResult) int {
		return left.index - right.index
	})

	merged := make(map[string][]string)
	for _, result := range resultSlice {
		if result.err != nil {
			return nil, result.err
		}
		mergePreScanPathResult(merged, result)
	}
	sortPreScanResults(merged)
	return merged, nil
}

// preScanOnePath dispatches one file to the language-specific import-map
// scanner without mutating shared result state.
func (e *Engine) preScanOnePath(resolvedRepoRoot string, rawPath string) (preScanPathResult, error) {
	resolvedPath, err := filepath.Abs(rawPath)
	if err != nil {
		return preScanPathResult{}, fmt.Errorf("resolve prescan path %q: %w", rawPath, err)
	}

	definition, ok := e.registry.LookupByPath(resolvedPath)
	if !ok {
		return preScanPathResult{}, nil
	}

	var names []string
	switch definition.Language {
	case "c":
		names, err = e.preScanC(resolvedPath)
	case "c_sharp":
		names, err = e.preScanCSharp(resolvedPath)
	case "cpp":
		names, err = e.preScanCPP(resolvedPath)
	case "dart":
		names, err = e.preScanDart(resolvedPath)
	case "elixir":
		names, err = e.preScanElixir(resolvedPath)
	case "haskell":
		names, err = e.preScanHaskell(resolvedPath)
	case "javascript":
		names, err = e.preScanJavaScriptLike(resolvedRepoRoot, resolvedPath, "javascript", "javascript")
	case "java":
		names, err = e.preScanJava(resolvedPath)
	case "java_metadata":
		names = nil
	case "kotlin":
		names, err = e.preScanKotlin(resolvedRepoRoot, resolvedPath)
	case "perl":
		names, err = e.preScanPerl(resolvedPath)
	case "php":
		names, err = e.preScanPHP(resolvedPath)
	case "python":
		names, err = e.preScanPython(resolvedPath)
	case "ruby":
		names, err = e.preScanRuby(resolvedPath)
	case "go":
		names, err = e.preScanGo(resolvedPath)
	case "groovy":
		names, err = e.preScanGroovy(resolvedPath)
	case "rust":
		names, err = e.preScanRust(resolvedPath)
	case "scala":
		names, err = e.preScanScala(resolvedPath)
	case "swift":
		names, err = e.preScanSwift(resolvedPath)
	case "tsx":
		names, err = e.preScanJavaScriptLike(resolvedRepoRoot, resolvedPath, "tsx", "tsx")
	case "typescript":
		names, err = e.preScanJavaScriptLike(resolvedRepoRoot, resolvedPath, "typescript", "typescript")
	default:
		return preScanPathResult{}, nil
	}
	if err != nil {
		return preScanPathResult{}, err
	}
	return preScanPathResult{path: resolvedPath, names: names}, nil
}

// mergePreScanPathResult appends one file's names into the shared import map.
func mergePreScanPathResult(results map[string][]string, result preScanPathResult) {
	if result.path == "" {
		return
	}
	for _, name := range result.names {
		results[name] = append(results[name], result.path)
	}
}

// sortPreScanResults keeps import-map paths stable across sequential and
// worker-aware execution.
func sortPreScanResults(results map[string][]string) {
	for _, paths := range results {
		slices.Sort(paths)
	}
}

func (e *Engine) parseDefinition(
	repoRoot string,
	definition Definition,
	resolvedPath string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	switch definition.Language {
	case "c":
		return e.parseC(resolvedPath, isDependency, options)
	case "c_sharp":
		return e.parseCSharp(resolvedPath, isDependency, options)
	case "cpp":
		return e.parseCPP(resolvedPath, isDependency, options)
	case "dart":
		return e.parseDart(resolvedPath, isDependency, options)
	case "dockerfile":
		return e.parseDockerfile(resolvedPath, isDependency, options)
	case "elixir":
		return e.parseElixir(resolvedPath, isDependency, options)
	case "haskell":
		return e.parseHaskell(resolvedPath, isDependency, options)
	case "javascript":
		return e.parseJavaScriptLike(repoRoot, resolvedPath, "javascript", "javascript", isDependency, options)
	case "json":
		return e.parseJSON(resolvedPath, isDependency, options)
	case "java":
		return e.parseJava(resolvedPath, isDependency, options)
	case "java_metadata":
		return parseJavaMetadata(resolvedPath, isDependency)
	case "kotlin":
		return e.parseKotlin(repoRoot, resolvedPath, isDependency, options)
	case "perl":
		return e.parsePerl(resolvedPath, isDependency, options)
	case "php":
		return e.parsePHP(resolvedPath, isDependency, options)
	case "python":
		return e.parsePython(repoRoot, resolvedPath, isDependency, options)
	case "ruby":
		return e.parseRuby(resolvedPath, isDependency, options)
	case "rust":
		return e.parseRust(repoRoot, resolvedPath, isDependency, options)
	case "go":
		return e.parseGo(resolvedPath, isDependency, options)
	case "hcl":
		return e.parseHCL(resolvedPath, isDependency, options)
	case "groovy":
		return e.parseGroovy(resolvedPath, isDependency, options)
	case "scala":
		return e.parseScala(resolvedPath, isDependency, options)
	case "sql":
		return e.parseSQL(resolvedPath, isDependency, options)
	case "swift":
		return e.parseSwift(resolvedPath, isDependency, options)
	case "tsx":
		return e.parseJavaScriptLike(repoRoot, resolvedPath, "tsx", "tsx", isDependency, options)
	case "typescript":
		return e.parseJavaScriptLike(repoRoot, resolvedPath, "typescript", "typescript", isDependency, options)
	case "raw_text":
		return parseRawText(resolvedPath, isDependency), nil
	case "yaml":
		return e.parseYAML(resolvedPath, isDependency, options)
	default:
		return nil, fmt.Errorf(
			"parser %q for language %q is not implemented in the native engine yet",
			definition.ParserKey,
			definition.Language,
		)
	}
}

func readSource(path string) ([]byte, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read source %q: %w", path, err)
	}
	return body, nil
}
