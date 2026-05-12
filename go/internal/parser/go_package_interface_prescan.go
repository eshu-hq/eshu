package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	golangparser "github.com/eshu-hq/eshu/go/internal/parser/golang"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// defaultPackagePrescanWorkerCap matches the existing snapshot parse-worker
// default (min(NumCPU, 8)) the collector applies to its file-level parse
// pool. Going above this would not help the Go package prescan because the
// dominant cost after the per-file parse collapse is file I/O, and APFS
// throughput tops out around this concurrency on M-series and Linux NVMe
// alike.
const defaultPackagePrescanWorkerCap = 8

// effectivePackagePrescanWorkers normalizes the worker count callers request.
// A non-positive configured value selects the default min(NumCPU, cap); any
// positive value is clamped to NumCPU*2 so a stale operator override cannot
// over-subscribe the host.
func effectivePackagePrescanWorkers(configured int) int {
	cpu := runtime.NumCPU()
	if cpu < 1 {
		cpu = 1
	}
	if configured <= 0 {
		if cpu < defaultPackagePrescanWorkerCap {
			return cpu
		}
		return defaultPackagePrescanWorkerCap
	}
	if configured > cpu*2 {
		return cpu * 2
	}
	return configured
}

// PreScanGoPackageSemanticRoots returns package-level Go reachability evidence
// that must be collected before per-file parsing. The result includes package
// import paths, imported interface parameter contracts, imported receiver call
// roots, chained interface-return receiver roots, and generic constraint roots.
// The collector feeds these contracts back into per-file parsing so symbol
// roots can be bounded by package and receiver evidence.
//
// This entrypoint preserves the original sequential signature for callers
// that have no opinion on worker count; it delegates to
// PreScanGoPackageSemanticRootsWithWorkers with a default of
// min(NumCPU, 8) workers, matching the snapshot parse pool the collector
// already runs.
func (e *Engine) PreScanGoPackageSemanticRoots(
	repoRoot string,
	paths []string,
) (GoPackageSemanticRoots, error) {
	return e.PreScanGoPackageSemanticRootsWithWorkers(repoRoot, paths, 0)
}

// PreScanGoPackageSemanticRootsWithWorkers is the worker-count-aware variant
// of PreScanGoPackageSemanticRoots. The per-file parse passes run on a pool
// of size effectivePackagePrescanWorkers(workers); zero or negative values
// select the default min(NumCPU, 8). Aggregation across files stays on the
// calling goroutine so the result is deterministic regardless of worker
// scheduling.
//
// The implementation parses each Go file once, builds the per-file evidence
// every package-level extractor needs, and feeds the result into a single
// sequential aggregation pass. Files whose package declares same-package
// interfaces with imported-receiver method returns get a second per-file
// parse to compute chained method call roots; that secondary pass also runs
// on the worker pool and is skipped entirely for packages without such
// interfaces. Before this shape, the function ran seven separate per-file
// parses sequentially regardless of package shape, which dominated the K8s
// parse-stage wall (ADR row 1818 follow-up).
func (e *Engine) PreScanGoPackageSemanticRootsWithWorkers(
	repoRoot string,
	paths []string,
	workers int,
) (GoPackageSemanticRoots, error) {
	resolvedRepoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve go package interface prescan repo root %q: %w", repoRoot, err)
	}

	workerCount := effectivePackagePrescanWorkers(workers)

	// Filter and resolve Go paths before launching workers so the pool only
	// dispatches real work and the post-parse aggregation loop has stable
	// inputs.
	prescanFiles := make([]prescanFile, 0, len(paths))
	packageDirs := make(map[string]struct{})
	for _, rawPath := range paths {
		resolvedPath, err := filepath.Abs(rawPath)
		if err != nil {
			return nil, fmt.Errorf("resolve go package interface prescan path %q: %w", rawPath, err)
		}
		rel, err := filepath.Rel(resolvedRepoRoot, resolvedPath)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		definition, ok := e.registry.LookupByPath(resolvedPath)
		if !ok || definition.Language != "go" {
			continue
		}
		packageDir := filepath.Dir(resolvedPath)
		packageDirs[packageDir] = struct{}{}
		prescanFiles = append(prescanFiles, prescanFile{
			resolvedPath: resolvedPath,
			packageDir:   packageDir,
		})
	}

	// Pass 1: one read+parse+walk per Go file collects every per-file evidence
	// type the package-level aggregation needs. Workers consume slice indices
	// and write evidence back to the same slot, so the post-parse aggregation
	// loop iterates prescanFiles in the original caller-supplied order — that
	// determinism keeps the aggregated GoImportedInterfaceParamMethods and
	// GoDirectMethodCallRoots slice orderings byte-identical to the previous
	// sequential shape.
	if err := e.runPackagePrescanPass(workerCount, prescanFiles, func(parser *tree_sitter.Parser, idx int) error {
		evidence, err := golangparser.PreScanFileEvidence(parser, prescanFiles[idx].resolvedPath)
		if err != nil {
			return err
		}
		prescanFiles[idx].evidence = evidence
		return nil
	}); err != nil {
		return nil, err
	}

	// Resolve module path and per-package import paths from the module root.
	modulePath := goModulePath(resolvedRepoRoot)
	packageImportPaths := make(map[string]string)
	if modulePath != "" {
		for packageDir := range packageDirs {
			if importPath, ok := goImportPathForDir(resolvedRepoRoot, modulePath, packageDir); ok {
				packageImportPaths[packageDir] = importPath
			}
		}
		for i := range prescanFiles {
			prescanFiles[i].importPath = packageImportPaths[prescanFiles[i].packageDir]
		}
	}

	// Seed result map with one options entry per Go package directory observed
	// in pass 1; the map keys remain stable across the rest of the function.
	results := make(GoPackageSemanticRoots)
	for packageDir := range packageDirs {
		options := results[packageDir]
		if options.ImportedInterfaceParamMethods == nil {
			options.ImportedInterfaceParamMethods = make(GoImportedInterfaceParamMethods)
		}
		if options.DirectMethodCallRoots == nil {
			options.DirectMethodCallRoots = make(GoDirectMethodCallRoots)
		}
		options.ImportPath = packageImportPaths[packageDir]
		results[packageDir] = options
	}

	// Aggregate pass 1 (consumer view) directly from per-file evidence.
	for _, fe := range prescanFiles {
		if len(fe.evidence.ImportedInterfaceParamMethods) == 0 {
			continue
		}
		options := results[fe.packageDir]
		mergeGoImportedInterfaceParamMethods(
			options.ImportedInterfaceParamMethods,
			GoImportedInterfaceParamMethods(fe.evidence.ImportedInterfaceParamMethods),
		)
		results[fe.packageDir] = options
	}

	// Aggregate pass 2 (producer view): qualified exported-interface param
	// methods keyed by importPath.functionName, then fan out to every package's
	// options so cross-package consumers see all known exports.
	if modulePath != "" {
		qualifiedTargets := make(GoImportedInterfaceParamMethods)
		for _, fe := range prescanFiles {
			if fe.importPath == "" {
				continue
			}
			for functionName, byIndex := range fe.evidence.ExportedInterfaceParamMethods {
				key := strings.ToLower(fe.importPath + "." + functionName)
				if _, ok := qualifiedTargets[key]; !ok {
					qualifiedTargets[key] = make(map[int][]string)
				}
				for index, methods := range byIndex {
					qualifiedTargets[key][index] = appendUniqueGoMethods(qualifiedTargets[key][index], methods)
				}
			}
		}
		for packageDir := range packageDirs {
			options := results[packageDir]
			mergeGoImportedInterfaceParamMethods(options.ImportedInterfaceParamMethods, qualifiedTargets)
			results[packageDir] = options
		}
	}

	// Aggregate pass 3 (direct method call roots with per-file interface
	// returns) and pass 4 (package-local interface imported-method returns).
	directMethodRoots := make(GoDirectMethodCallRoots)
	packageInterfaceReturns := make(map[string]map[string]string)
	for _, fe := range prescanFiles {
		mergeGoDirectMethodCallRoots(
			directMethodRoots,
			GoDirectMethodCallRoots(fe.evidence.ImportedDirectMethodCallRoots),
		)
		if len(fe.evidence.LocalInterfaceImportedMethodReturns) == 0 {
			continue
		}
		if packageInterfaceReturns[fe.packageDir] == nil {
			packageInterfaceReturns[fe.packageDir] = make(map[string]string)
		}
		for key, typeName := range fe.evidence.LocalInterfaceImportedMethodReturns {
			packageInterfaceReturns[fe.packageDir][key] = typeName
		}
	}

	// Pass 5 (chained method call roots using package-level interface returns)
	// needs the pass-4 aggregate, so it runs as a second per-file parse for any
	// file whose package has non-empty interface returns. Packages without such
	// interfaces skip this pass entirely. Workers write each file's roots into
	// the indexed slot so the post-parse merge stays deterministic.
	chainedPerFile := make([]golangparser.GoDirectMethodCallRoots, len(prescanFiles))
	if err := e.runPackagePrescanPass(workerCount, prescanFiles, func(parser *tree_sitter.Parser, idx int) error {
		interfaceReturns := packageInterfaceReturns[prescanFiles[idx].packageDir]
		if len(interfaceReturns) == 0 {
			return nil
		}
		fileRoots, err := golangparser.ImportedDirectMethodCallRootsWithInterfaceReturns(parser, prescanFiles[idx].resolvedPath, interfaceReturns)
		if err != nil {
			return err
		}
		chainedPerFile[idx] = fileRoots
		return nil
	}); err != nil {
		return nil, err
	}
	chainedDirectMethodRoots := make(GoDirectMethodCallRoots)
	for _, fileRoots := range chainedPerFile {
		if len(fileRoots) == 0 {
			continue
		}
		mergeGoDirectMethodCallRoots(chainedDirectMethodRoots, GoDirectMethodCallRoots(fileRoots))
	}
	mergeGoDirectMethodCallRoots(directMethodRoots, chainedDirectMethodRoots)
	for packageDir, importPath := range packageImportPaths {
		options := results[packageDir]
		mergeGoDirectMethodCallRootsForImportPath(options.DirectMethodCallRoots, directMethodRoots, importPath)
		results[packageDir] = options
	}

	// Aggregate passes 6/7/8 (generic constraint methods) from per-file
	// evidence. Same constraint-resolution shape as the previous loop in
	// mergeGoPackageGenericConstraintMethodRoots; the difference is that the
	// per-file slices and maps are no longer recomputed by re-parsing each
	// file.
	packageInterfaces := make(map[string]map[string][]string)
	packageConstraints := make(map[string][]string)
	packageMethods := make(map[string][]string)
	for _, fe := range prescanFiles {
		if len(fe.evidence.LocalInterfaceMethods) > 0 && packageInterfaces[fe.packageDir] == nil {
			packageInterfaces[fe.packageDir] = make(map[string][]string)
		}
		for name, methods := range fe.evidence.LocalInterfaceMethods {
			packageInterfaces[fe.packageDir][name] = appendUniqueGoMethods(packageInterfaces[fe.packageDir][name], methods)
		}
		packageConstraints[fe.packageDir] = appendUniqueGoMethods(packageConstraints[fe.packageDir], fe.evidence.GenericConstraintInterfaceNames)
		packageMethods[fe.packageDir] = appendUniqueGoMethods(packageMethods[fe.packageDir], fe.evidence.MethodDeclarationKeys)
	}
	for packageDir, importPath := range packageImportPaths {
		options := results[packageDir]
		if options.DirectMethodCallRoots == nil {
			options.DirectMethodCallRoots = make(GoDirectMethodCallRoots)
		}
		for _, constraint := range packageConstraints[packageDir] {
			requiredMethods := packageInterfaces[packageDir][constraint]
			if len(requiredMethods) == 0 {
				continue
			}
			for _, methodKey := range packageMethods[packageDir] {
				_, methodName, ok := strings.Cut(methodKey, ".")
				if !ok || !goMethodListContains(requiredMethods, methodName) {
					continue
				}
				qualifiedKey := strings.ToLower(importPath + "." + methodKey)
				options.DirectMethodCallRoots[qualifiedKey] = appendUniqueGoMethods(
					options.DirectMethodCallRoots[qualifiedKey],
					[]string{"go.generic_constraint_method"},
				)
			}
		}
		results[packageDir] = options
	}

	return results, nil
}

func goMethodListContains(methods []string, method string) bool {
	normalized := strings.ToLower(strings.TrimSpace(method))
	for _, candidate := range methods {
		if candidate == normalized {
			return true
		}
	}
	return false
}

// prescanFile is the per-file working set the parent passes between worker
// pool steps. Declared at file scope so the worker-pool helper can name the
// type in its signature; pass-1 workers populate the evidence slot, and the
// post-parse aggregation loop in PreScanGoPackageSemanticRootsWithWorkers
// reads each slot in original order.
type prescanFile struct {
	resolvedPath string
	packageDir   string
	importPath   string
	evidence     *golangparser.PrescanFileEvidence
}

// runPackagePrescanPass dispatches one job per file index across a worker
// pool capped to the number of files. Each worker owns its own tree-sitter
// parser (tree-sitter parsers are not safe for concurrent use) and reuses it
// across every file it processes. The first non-nil error any worker returns
// is reported back to the caller after all workers finish.
//
// Workers do not synchronize on file results — work is expected to write to
// pre-allocated slots in a caller-owned slice indexed by job index, which
// preserves byte-identical aggregation order across runs.
func (e *Engine) runPackagePrescanPass(
	workers int,
	files []prescanFile,
	work func(parser *tree_sitter.Parser, idx int) error,
) error {
	workers = packagePrescanPassWorkerCount(workers, len(files))
	if workers == 0 {
		return nil
	}

	jobs := make(chan int, len(files))
	for i := range files {
		jobs <- i
	}
	close(jobs)

	var (
		wg       sync.WaitGroup
		errMu    sync.Mutex
		firstErr error
	)
	setErr := func(err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
	}

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			parser, err := e.runtime.Parser("go")
			if err != nil {
				setErr(err)
				return
			}
			defer parser.Close()
			for idx := range jobs {
				if err := work(parser, idx); err != nil {
					setErr(err)
				}
			}
		}()
	}
	wg.Wait()
	return firstErr
}

func packagePrescanPassWorkerCount(workers int, fileCount int) int {
	if fileCount <= 0 {
		return 0
	}
	if workers < 1 {
		return 1
	}
	if workers > fileCount {
		return fileCount
	}
	return workers
}

func goModulePath(repoRoot string) string {
	body, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return strings.TrimSpace(fields[1])
		}
	}
	return ""
}

func goImportPathForDir(repoRoot string, modulePath string, packageDir string) (string, bool) {
	rel, err := filepath.Rel(repoRoot, packageDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	if rel == "." {
		return modulePath, true
	}
	return modulePath + "/" + filepath.ToSlash(rel), true
}

func mergeGoImportedInterfaceParamMethods(
	target GoImportedInterfaceParamMethods,
	source GoImportedInterfaceParamMethods,
) {
	for functionName, byIndex := range source {
		if _, ok := target[functionName]; !ok {
			target[functionName] = make(map[int][]string)
		}
		for index, methods := range byIndex {
			target[functionName][index] = appendUniqueGoMethods(target[functionName][index], methods)
		}
	}
}

func appendUniqueGoMethods(target []string, methods []string) []string {
	for _, method := range methods {
		trimmed := strings.TrimSpace(strings.ToLower(method))
		if trimmed == "" {
			continue
		}
		found := false
		for _, existing := range target {
			if existing == trimmed {
				found = true
				break
			}
		}
		if !found {
			target = append(target, trimmed)
		}
	}
	return target
}

func mergeGoDirectMethodCallRoots(target GoDirectMethodCallRoots, source GoDirectMethodCallRoots) {
	for key, kinds := range source {
		target[key] = appendUniqueGoMethods(target[key], kinds)
	}
}

func mergeGoDirectMethodCallRootsForImportPath(
	target GoDirectMethodCallRoots,
	source GoDirectMethodCallRoots,
	importPath string,
) {
	prefix := strings.ToLower(strings.TrimSpace(importPath)) + "."
	for key, kinds := range source {
		if strings.HasPrefix(key, prefix) {
			target[key] = appendUniqueGoMethods(target[key], kinds)
		}
	}
}
