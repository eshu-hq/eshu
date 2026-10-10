// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func runQueueSelect(args []string) error {
	fs := flag.NewFlagSet("queue-select", flag.ContinueOnError)
	registry := fs.String("registry", "", "path to the merge-group head gate registry")
	trustedRegistry := fs.String("trusted-registry", "", "optional default-branch registry to union with the head registry")
	repoRoot := fs.String("repo-root", "", "repository root")
	repo := fs.String("repo", "", "GitHub repository in owner/name form")
	baseRef := fs.String("base-ref", "", "base branch targeted by the queue branch")
	branch := fs.String("merge-group-branch", "", "queue branch carrying the fixed base SHA")
	headSHA := fs.String("head-sha", "", "merge-group commit SHA")
	workflow := fs.String("workflow", "", "workflow filename under .github/workflows")
	job := fs.String("job", "", "optional registry ci.job or concrete ci.check_names identity")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}
	if *registry == "" || *repoRoot == "" || *repo == "" || *baseRef == "" || *branch == "" || *headSHA == "" || *workflow == "" {
		return fmt.Errorf("--registry, --repo-root, --repo, --base-ref, --merge-group-branch, --head-sha, and --workflow are required")
	}
	if filepath.Base(*workflow) != *workflow || strings.TrimSpace(*workflow) != *workflow {
		return fmt.Errorf("--workflow must be a filename under .github/workflows")
	}
	if _, err := resolveRepoRoot(*repoRoot); err != nil {
		return fmt.Errorf("resolve repo root: %w", err)
	}
	registries := make([]*cigates.Registry, 0, 2)
	for _, path := range []string{*registry, *trustedRegistry} {
		if path == "" {
			continue
		}
		reg, err := cigates.Load(path)
		if err != nil {
			return fmt.Errorf("load registry %s: %w", path, err)
		}
		registries = append(registries, reg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	selection, err := selectQueueGates(ctx, execGHRunner{}, registries, *repo, *baseRef, *branch, *headSHA, *workflow, *job)
	if err != nil {
		return err
	}
	return writeQueueSelection(os.Stdout, selection)
}

func writeQueueSelection(out io.Writer, selection cigates.QueueSelection) error {
	return json.NewEncoder(out).Encode(selection)
}

// selectQueueGates reuses await's fixed-base GitHub compare. Both registries
// contribute rows so an unmerged registry edit cannot remove a gate that the
// trusted default-branch publisher will still require.
func selectQueueGates(ctx context.Context, runner ghRunner, registries []*cigates.Registry, repo, baseRef, branch, headSHA, workflow, job string) (cigates.QueueSelection, error) {
	paths, truncated, err := mergeGroupChangedPaths(ctx, runner, repo, baseRef, branch, headSHA)
	if err != nil {
		return cigates.QueueSelection{}, err
	}
	return cigates.SelectQueueWorkflow(registries, paths, truncated, workflow, job)
}
