// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// derivedPreScanLanguages is the exact set of parser-registry languages whose
// PreScan-equivalent ImportsMap contribution can be derived from the same
// parse payload the collector's parse stage already computes, instead of
// running a second dedicated tree-sitter PreScan pass over the file (#4764).
// Scoped to php, javascript, typescript, and tsx: for these languages the
// "functions", "classes", "interfaces", and "traits" parse buckets are a
// proven (shim-verified, 0/0 symmetric set difference) superset-equivalent of
// the names PreScan collects. json and groovy keep their own prescan/parse
// relationship and are intentionally excluded.
var derivedPreScanLanguages = map[string]bool{
	"php":        true,
	"javascript": true,
	"typescript": true,
	"tsx":        true,
}

// IsDerivedPreScanLanguage reports whether language's ImportsMap contribution
// can be derived from a parse payload via DerivePreScanNames instead of
// requiring a dedicated PreScan pass. See derivedPreScanLanguages for scope
// and proof provenance.
func IsDerivedPreScanLanguage(language string) bool {
	return derivedPreScanLanguages[language]
}

// derivePreScanBuckets lists the parse payload buckets whose item names
// reproduce what the legacy tree-sitter PreScan pass collects for the
// languages in derivedPreScanLanguages. This mirrors the equivalence proven
// by the #4764 shim: PHP class/interface/trait/function/method declarations
// land in "classes"/"interfaces"/"traits"/"functions", and PHP anonymous
// classes and JavaScript-family function-valued pair/assignment/variable
// exports all land in "functions" or "classes" via the same synthesized
// names (e.g. phpAnonymousClassName) PreScan uses.
var derivePreScanBuckets = []string{"functions", "classes", "interfaces", "traits"}

// DerivePreScanNames extracts the PreScan-equivalent declaration names from a
// parse payload produced by ParsePath, for use only when the payload's
// language is IsDerivedPreScanLanguage. Names are deduplicated and
// filepath.Clean-normalized identically to the legacy PreScan path so a
// caller can substitute this for a dedicated PreScan call without changing
// ImportsMap output.
func DerivePreScanNames(payload map[string]any) []string {
	seen := make(map[string]struct{})
	var names []string
	for _, bucket := range derivePreScanBuckets {
		items, ok := payload[bucket].([]map[string]any)
		if !ok {
			continue
		}
		for _, item := range items {
			rawName, ok := item["name"].(string)
			if !ok {
				continue
			}
			name := strings.TrimSpace(rawName)
			if name == "" {
				continue
			}
			cleaned := filepath.Clean(name)
			if _, exists := seen[cleaned]; exists {
				continue
			}
			seen[cleaned] = struct{}{}
			names = append(names, cleaned)
		}
	}
	return names
}

// derivedLanguagePreScanDispatchCount counts calls into preScanOnePath's
// dedicated tree-sitter PreScan dispatch for a parser.IsDerivedPreScanLanguage
// language (php, javascript, typescript, tsx). Test-only: production code
// never reads it. It exists so a regression test can assert this count stays
// at zero for a full-ingest collector run, proving the #4764 dedup (deriving
// ImportsMap for these languages from the parse-stage payload instead) has
// not silently regressed back into a second tree-sitter parse per file. An
// atomic keeps this safe to read from a test while collector workers may
// still be dispatching prescan for other, non-derived languages concurrently.
var derivedLanguagePreScanDispatchCount atomic.Int64

// ResetDerivedLanguagePreScanDispatchCountForTest zeroes
// derivedLanguagePreScanDispatchCount and returns its value beforehand.
// Test-only.
func ResetDerivedLanguagePreScanDispatchCountForTest() int64 {
	return derivedLanguagePreScanDispatchCount.Swap(0)
}

// DerivedLanguagePreScanDispatchCountForTest reads
// derivedLanguagePreScanDispatchCount without resetting it. Test-only.
func DerivedLanguagePreScanDispatchCountForTest() int64 {
	return derivedLanguagePreScanDispatchCount.Load()
}

// PreScanSkip names one path a pre-scan skipped instead of failing on. Path is
// the path exactly as the caller passed it; Err wraps shared.ErrSourceTooLarge.
type PreScanSkip struct {
	Path string
	Err  error
}

// PreScanRepositoryPathsSkippingOversizeStats is
// PreScanRepositoryPathsWithWorkersStats with one difference: a file whose
// pre-scan read returns an error wrapping shared.ErrSourceTooLarge is skipped,
// not a failure of the whole scan. Discovery sized the file under the read cap
// and it grew past it before the pre-scan read it. The skipped paths come back
// in input order, and the file adds nothing to the import map or the stats.
// Every other per-file error still fails the scan with the first one in input
// order, exactly as the strict variant does.
func (e *Engine) PreScanRepositoryPathsSkippingOversizeStats(
	repoRoot string, paths []string, workers int,
) (map[string][]string, []PreScanFileStat, []PreScanSkip, error) {
	return e.preScanPathsWithRoot(repoRoot, paths, workers, true)
}

// skipsPreScanPath reports whether a per-file pre-scan error is one the
// caller opted to skip: only a file over the read cap, and only when the scan
// was asked to tolerate it.
func skipsPreScanPath(tolerateOversize bool, err error) bool {
	return tolerateOversize && errors.Is(err, shared.ErrSourceTooLarge)
}

// preScanPathsSequential preserves the historical first-error behavior for
// callers that do not opt into the worker-aware repository pre-scan path. With
// tolerateOversize set, a file over the read cap is recorded as a skip and the
// scan continues.
func (e *Engine) preScanPathsSequential(
	resolvedRepoRoot string, paths []string, tolerateOversize bool,
) (map[string][]string, []PreScanFileStat, []PreScanSkip, error) {
	results := make(map[string][]string)
	stats := make([]PreScanFileStat, 0, len(paths))
	var skips []PreScanSkip
	for _, rawPath := range paths {
		scanned, err := e.preScanOnePath(resolvedRepoRoot, rawPath)
		if err != nil {
			if skipsPreScanPath(tolerateOversize, err) {
				skips = append(skips, PreScanSkip{Path: rawPath, Err: err})
				continue
			}
			return nil, nil, nil, err
		}
		mergePreScanPathResult(results, scanned)
		if stat, ok := scanned.stat(); ok {
			stats = append(stats, stat)
		}
	}
	sortPreScanResults(results)
	return results, stats, skips, nil
}

// preScanPathResult carries one file's resolved path and discovered names so
// concurrent workers can merge results after sorting by original input order.
type preScanPathResult struct {
	index           int
	path            string
	names           []string
	language        string
	durationSeconds float64
	dispatched      bool
	err             error
}

// stat converts a preScanPathResult into a PreScanFileStat when the file
// actually dispatched to a language pre-scanner (dispatched is false for
// unregistered files and languages without pre-scan support, so those never
// appear as a spurious zero-duration language sample).
func (r preScanPathResult) stat() (PreScanFileStat, bool) {
	if !r.dispatched {
		return PreScanFileStat{}, false
	}
	return PreScanFileStat{
		Path:            r.path,
		Language:        r.language,
		DurationSeconds: r.durationSeconds,
	}, true
}

// preScanPathsConcurrent scans files in parallel, then returns the first input
// order error and sorted per-name path lists to keep output deterministic.
// With tolerateOversize set, a file over the read cap is recorded as a skip in
// input order instead of being the first error.
func (e *Engine) preScanPathsConcurrent(
	resolvedRepoRoot string, paths []string, workers int, tolerateOversize bool,
) (map[string][]string, []PreScanFileStat, []PreScanSkip, error) {
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
	stats := make([]PreScanFileStat, 0, len(resultSlice))
	var skips []PreScanSkip
	for _, result := range resultSlice {
		if result.err != nil {
			if skipsPreScanPath(tolerateOversize, result.err) {
				skips = append(skips, PreScanSkip{Path: paths[result.index], Err: result.err})
				continue
			}
			return nil, nil, nil, result.err
		}
		mergePreScanPathResult(merged, result)
		if stat, ok := result.stat(); ok {
			stats = append(stats, stat)
		}
	}
	sortPreScanResults(merged)
	return merged, stats, skips, nil
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
