// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// probeSmoke reports whether this head is a clean rebase whose heavy
// verdicts may transfer from the previous head (#8617 step 5). All must
// hold: the head came from a force-push (a rebase, not a new push); the
// replaced head was fully green on every required context; nothing
// that changed between the two heads touches a file this PR changes or
// any gate definition; and the candidate's own success came from a
// full gate run (or tree-identical reuse of one), never from an
// earlier smoke pass.
//
// The last two conditions bound, but do not eliminate, semantic risk:
// heavy gates evaluate the whole merged tree, and a disjoint base
// change (each of which was itself gated when it merged) can still
// interact with the PR's behavior. The accepted bound is one hop —
// heavy verdicts transfer at most once from a fully run tree — the
// fast subset still runs on the new tree, and the full set re-runs on
// the merge result via Main Health, where a miss surfaces.
func probeSmoke(ctx context.Context, runner ghRunner, repo string, pr int, headSHA string, changedPaths, contexts []string) (bool, error) {
	if !validRepoName(repo) {
		return false, fmt.Errorf("invalid --repo %q", repo)
	}
	if !commitSHAPattern.MatchString(headSHA) {
		return false, fmt.Errorf("invalid --head-sha %q", headSHA)
	}
	if len(contexts) == 0 {
		return false, fmt.Errorf("no required status contexts to transfer against")
	}
	candidate, err := forcePushBefore(ctx, runner, repo, pr, headSHA)
	if err != nil {
		return false, err
	}
	if candidate == "" || !commitSHAPattern.MatchString(candidate) {
		return false, nil
	}
	delta, err := compareFiles(ctx, runner, repo, candidate, headSHA)
	if err != nil {
		return false, err
	}
	if delta == nil { // compare truncated: cannot prove disjointness
		return false, nil
	}
	changed := make(map[string]bool, len(changedPaths))
	for _, path := range changedPaths {
		changed[path] = true
	}
	for _, file := range delta {
		// Gate-definition surfaces refuse transfer too: a workflow,
		// registry, or gate-script change means the old verdicts were
		// produced by different definitions than the new tree would run.
		if strings.HasPrefix(file, ".github/") ||
			strings.HasPrefix(file, "specs/ci-gates") ||
			strings.HasPrefix(file, "scripts/") ||
			changed[file] {
			return false, nil
		}
	}
	green, err := requiredContextsGreen(ctx, runner, repo, candidate, contexts)
	if err != nil || !green {
		return green, err
	}
	return candidateRanFull(ctx, runner, repo, candidate)
}

// candidateRanFull requires the candidate's aggregating status to name
// a full gate run or a tree-identical reuse of one. A smoke-passed
// candidate (or any unrecognized description, including none) refuses,
// so transferred verdicts can never chain: each smoke hop must start
// from a tree whose heavy gates actually ran.
func candidateRanFull(ctx context.Context, runner ghRunner, repo, candidate string) (bool, error) {
	description, found, err := latestStatusDescription(ctx, runner, repo, candidate, "required-gates-complete")
	if err != nil || !found {
		return false, err
	}
	switch description {
	case "All path-selected blocking gates passed",
		"Tree-identical reuse: verdict copied from a previous head (see run log)":
		return true, nil
	}
	return false, nil
}

type statusDescriptionEntry struct {
	Context     string `json:"context"`
	Description string `json:"description"`
}

// latestStatusDescription returns the newest status description for a
// context. The endpoint returns newest first, so the first match wins.
func latestStatusDescription(ctx context.Context, runner ghRunner, repo, sha, contextName string) (string, bool, error) {
	out, err := runner.Run(ctx, "api", "repos/"+repo+"/commits/"+sha+"/statuses?per_page=100")
	if err != nil {
		return "", false, fmt.Errorf("read commit statuses: %w", err)
	}
	var entries []statusDescriptionEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		return "", false, fmt.Errorf("parse commit statuses: %w", err)
	}
	for _, entry := range entries {
		if entry.Context == contextName {
			return entry.Description, true, nil
		}
	}
	return "", false, nil
}

type compareResponse struct {
	Files []struct {
		Filename         string `json:"filename"`
		PreviousFilename string `json:"previous_filename"`
	} `json:"files"`
}

// compareFiles lists files changed between two commits. A nil slice
// (with nil error) means the response may be truncated (the request
// pages at 100 and the API caps compare files at 300), where absence
// of an overlap cannot be proven.
func compareFiles(ctx context.Context, runner ghRunner, repo, base, head string) ([]string, error) {
	out, err := runner.Run(ctx, "api", "repos/"+repo+"/compare/"+base+"..."+head+"?per_page=100")
	if err != nil {
		return nil, fmt.Errorf("compare %s...%s: %w", base, head, err)
	}
	var response compareResponse
	if err := json.Unmarshal(out, &response); err != nil {
		return nil, fmt.Errorf("parse compare %s...%s: %w", base, head, err)
	}
	if len(response.Files) >= 100 {
		return nil, nil
	}
	files := make([]string, 0, len(response.Files))
	for _, file := range response.Files {
		files = append(files, file.Filename)
		if file.PreviousFilename != "" {
			files = append(files, file.PreviousFilename)
		}
	}
	return files, nil
}

// smokeSubset keeps the resolved jobs that must still run on a smoke
// head: a job stays when any gate it covers is a fast local tier
// (pre-commit or pre-push) or is unknown to the registry, since an
// unknown gate cannot be proven heavy. Jobs covering only heavier
// gates transfer their verdict from the green candidate instead.
func smokeSubset(reg *cigates.Registry, resolved []resolvedRequiredGate) []resolvedRequiredGate {
	tiers := make(map[string]cigates.Tier, len(reg.Gates))
	for _, gate := range reg.Gates {
		tiers[gate.ID] = gate.Tier
	}
	kept := make([]resolvedRequiredGate, 0, len(resolved))
	for _, gate := range resolved {
		if smokeKeeps(tiers, gate.GateIDs) {
			kept = append(kept, gate)
		}
	}
	return kept
}

func smokeKeeps(tiers map[string]cigates.Tier, gateIDs []string) bool {
	for _, id := range gateIDs {
		tier, known := tiers[id]
		if !known {
			return true
		}
		if tier == cigates.TierPreCommit || tier == cigates.TierPrePush {
			return true
		}
	}
	return false
}
