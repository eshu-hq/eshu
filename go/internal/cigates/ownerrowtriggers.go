// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// checkOwnerRowTriggers is the trigger half of drift check 13. A blocking
// CI-only gate (Local nil) exists to own a CI job, and `ci-gates await` selects
// it from its triggers. A literal trigger the workflow's pull_request filter
// does not start on selects a check that never arrives for a PR that touches
// only that file: await then reports the check MISSING and waits out its
// timeout. The replay-coverage static mirror carried such a trigger
// (scripts/lib/go-test-run-guard.sh).
//
// Both filter forms are read. With `pull_request.paths:` the workflow starts
// when a changed file matches an entry, so a trigger no entry matches is
// flagged. With `pull_request.paths-ignore:` the workflow starts unless EVERY
// changed file matches an ignore entry, so a trigger that matches an entry is
// flagged, naming that entry. GitHub rejects both on one event, so a workflow
// that declares both never starts and is flagged as a whole (fail closed).
//
// Scope, on purpose: only CI-only rows, because they have no local command that
// runs the file anyway, and only literal triggers, because a glob needs a path
// universe to judge. A workflow with neither filter starts on every PR and is
// skipped. Rows that run locally and share a job with another row are not
// judged here.
//
// Matching follows GitHub: entries are applied in order, a `!` entry removes
// what the earlier entries added.
func checkOwnerRowTriggers(repoRoot string, reg *Registry) []error {
	cache := make(map[string]pullRequestFilter)
	var errs []error
	for _, g := range reg.Gates {
		if !g.Blocking || g.Local != nil || g.CI.Workflow == "" {
			continue
		}
		filter, ok := cache[g.CI.Workflow]
		if !ok {
			filter = readPullRequestFilter(repoRoot, g.CI.Workflow)
			cache[g.CI.Workflow] = filter
		}
		if len(filter.Paths) > 0 && len(filter.Ignore) > 0 {
			errs = append(errs, fmt.Errorf(
				"drift: owner row trigger: gate %q is owned by %s, which declares both paths: and paths-ignore: "+
					"on pull_request; GitHub rejects that, so the workflow never starts and await waits out its timeout",
				g.ID, g.CI.Workflow,
			))
			continue
		}
		for _, trigger := range g.Triggers {
			if strings.ContainsAny(trigger, "*?[") {
				continue
			}
			if len(filter.Paths) > 0 {
				if _, matched := matchingPathEntry(filter.Paths, trigger); !matched {
					errs = append(errs, fmt.Errorf(
						"drift: owner row trigger: gate %q lists %q, but no pull_request paths: entry of %s matches it, "+
							"so a PR touching only that file selects a check the workflow never starts and await waits "+
							"out its timeout; add the path to the workflow's paths: or drop it from the triggers",
						g.ID, trigger, g.CI.Workflow,
					))
				}
				continue
			}
			if entry, ignored := matchingPathEntry(filter.Ignore, trigger); ignored {
				errs = append(errs, fmt.Errorf(
					"drift: owner row trigger: gate %q lists %q, but the pull_request paths-ignore: entry %q of %s "+
						"matches it, so a PR touching only that file selects a check the workflow never starts and "+
						"await waits out its timeout; drop the path from the triggers or remove the matching paths-ignore entry",
					g.ID, trigger, entry, g.CI.Workflow,
				))
			}
		}
	}
	return errs
}

// pullRequestFilter is the path filter of a workflow's pull_request event.
type pullRequestFilter struct {
	Paths  []string
	Ignore []string
}

// readPullRequestFilter returns the `on.pull_request` paths and paths-ignore
// entries of workflow, or the zero filter when the file is unreadable or has
// neither.
func readPullRequestFilter(repoRoot, workflow string) pullRequestFilter {
	path := filepath.Join(repoRoot, ".github", "workflows", filepath.Base(workflow))
	raw, err := os.ReadFile(path) // #nosec G304 -- basename-confined workflow from the committed registry
	if err != nil {
		return pullRequestFilter{}
	}
	var wf struct {
		On struct {
			PullRequest struct {
				Paths  []string `yaml:"paths"`
				Ignore []string `yaml:"paths-ignore"`
			} `yaml:"pull_request"`
		} `yaml:"on"`
	}
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		// A shorthand `on: [push]` or `on: push` does not decode into the map
		// form; it has no path filter either.
		return pullRequestFilter{}
	}
	return pullRequestFilter{Paths: wf.On.PullRequest.Paths, Ignore: wf.On.PullRequest.Ignore}
}

// matchingPathEntry applies GitHub's ordered path-filter semantics to one
// changed path and returns the last positive entry that selected it, with
// whether the path is selected after the `!` entries ran.
func matchingPathEntry(patterns []string, changed string) (string, bool) {
	entry, matched := "", false
	for _, pattern := range patterns {
		if negated, ok := strings.CutPrefix(pattern, "!"); ok {
			if MatchGlob(negated, changed) {
				entry, matched = "", false
			}
			continue
		}
		if MatchGlob(pattern, changed) {
			entry, matched = pattern, true
		}
	}
	return entry, matched
}
