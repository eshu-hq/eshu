// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func runRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	registry := fs.String("registry", "", "path to ci-gates.v1.yaml registry")
	tier := fs.String("tier", "pre-pr", "tier ceiling (pre-commit|pre-push|pre-pr|ci-heavy|manual)")
	base := fs.String("base", "origin/main", "git base ref for changed-path detection")
	pathsFrom := fs.String("paths-from", "", "file of changed paths, one per line ('-' for stdin)")
	repoRoot := fs.String("repo-root", "", "repository root to run gate commands from (default: git toplevel)")
	category := fs.String("category", "", "comma-separated category filter (e.g. exactness,telemetry); empty = all")
	selfTests := fs.String("self-tests", "all", "self-test policy: all or changed")
	blockingOnly := fs.Bool("blocking-only", false, "run only blocking selected gates")
	reportFile := fs.String("report-file", "", "write an atomic JSON timing report to this path")
	_ = fs.Bool("json", false, "reserved for compatibility; use --report-file for structured output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *registry == "" {
		return fmt.Errorf("--registry is required")
	}

	// Gate commands in the registry are repo-root-relative ("bash scripts/...",
	// "cd go && ..."). Resolve the repo root so they run from there regardless of
	// this process's own working directory (e.g. wrappers invoke us via
	// `go -C go run`, which would otherwise leave commands running from go/).
	root, err := resolveRepoRoot(*repoRoot)
	if err != nil {
		return fmt.Errorf("resolve repo root: %w", err)
	}

	reg, err := cigates.Load(*registry)
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}
	changed, err := resolveChangedPaths(*pathsFrom, *base)
	if err != nil {
		return fmt.Errorf("resolve changed paths: %w", err)
	}
	cats, err := parseCategories(*category)
	if err != nil {
		return err
	}
	sels := cigates.FilterByCategory(reg.Select(changed, cigates.Tier(*tier)), cats)
	policy := selfTestPolicy(*selfTests)
	if policy != selfTestsAll && policy != selfTestsChanged {
		return fmt.Errorf("--self-tests must be %q or %q", selfTestsAll, selfTestsChanged)
	}
	report, runErr := executeGatesWithOptions(os.Stdout, sels, root, executeOptions{
		changedPaths: changed,
		selfTests:    policy,
		blockingOnly: *blockingOnly,
	})
	if reportErr := writeGateRunReport(*reportFile, report); reportErr != nil {
		if runErr != nil {
			return fmt.Errorf("%v; write report: %w", runErr, reportErr)
		}
		return reportErr
	}
	return runErr
}
