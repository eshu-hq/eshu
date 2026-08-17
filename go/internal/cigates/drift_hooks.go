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

// This file holds DriftCheck's checks 1 and 2 (pre-commit hook parsing and
// the hook-registration/hook-stage-consistency checks). Split out of
// drift.go, which was at the repository's 500-line cap, when check 11
// (checkCIScriptTriggerCoverage, scripttrigger.go) pushed it over -- the same
// class of split this package already applies elsewhere (see
// scripttrigger.go's own header on trivyskipdirs.go). DriftCheck's own doc
// comment in drift.go remains the single source of truth for what checks 1
// and 2 assert; this file only holds their implementation.

// ─── pre-commit hook parsing ────────────────────────────────────────────────

// hookEntry is a parsed local hook from .pre-commit-config.yaml.
type hookEntry struct {
	ID     string
	Stages []string
}

// preCommitFile is the minimal shape of .pre-commit-config.yaml we need.
type preCommitFile struct {
	Repos []struct {
		Repo  string `yaml:"repo"`
		Hooks []struct {
			ID     string   `yaml:"id"`
			Stages []string `yaml:"stages"`
		} `yaml:"hooks"`
	} `yaml:"repos"`
}

// parsePreCommitHooks reads .pre-commit-config.yaml under repoRoot and returns
// the map of hook id → hookEntry for every hook in a "local" repo block.
func parsePreCommitHooks(repoRoot string) (map[string]hookEntry, []error) {
	p := filepath.Join(repoRoot, ".pre-commit-config.yaml")
	raw, err := os.ReadFile(p) // #nosec G304 -- repoRoot is the operator-provided repo root
	if err != nil {
		return nil, []error{fmt.Errorf("drift: read %s: %w", p, err)}
	}
	var pcf preCommitFile
	if err := yaml.Unmarshal(raw, &pcf); err != nil {
		return nil, []error{fmt.Errorf("drift: parse %s: %w", p, err)}
	}

	hooks := make(map[string]hookEntry)
	for _, repo := range pcf.Repos {
		if repo.Repo != "local" {
			continue
		}
		for _, h := range repo.Hooks {
			id := strings.TrimSpace(h.ID)
			if id == "" {
				continue
			}
			hooks[id] = hookEntry{ID: id, Stages: h.Stages}
		}
	}
	return hooks, nil
}

// ─── check 1: hook → registry/hygiene ──────────────────────────────────────

func checkHookRegistration(hooks map[string]hookEntry, reg *Registry) []error {
	// Build lookup sets.
	gateHookIDs := make(map[string]struct{}, len(reg.Gates))
	for _, g := range reg.Gates {
		if g.HookID != "" {
			gateHookIDs[g.HookID] = struct{}{}
		}
	}
	hygieneIDs := make(map[string]struct{}, len(reg.HygieneHooks))
	for _, h := range reg.HygieneHooks {
		hygieneIDs[h.ID] = struct{}{}
	}

	var errs []error
	for id := range hooks {
		_, isGate := gateHookIDs[id]
		_, isHygiene := hygieneIDs[id]
		if !isGate && !isHygiene {
			errs = append(errs, fmt.Errorf(
				"drift: hook %q is neither a registered gate (hook_id) nor a declared hygiene hook; "+
					"add hook_id to a gate or add it to hygiene_hooks with a reason",
				id,
			))
		}
	}
	return errs
}

// ─── check 2: gate hook_id → present + stage match ─────────────────────────

// stageConsistentWithTier reports whether the hook's declared stages are
// consistent with the gate's tier. A gate with no stages declared (pre-commit
// default) is treated as running at the default stage, which is consistent with
// TierPreCommit but not TierPrePush.
func stageConsistentWithTier(stages []string, tier Tier) bool {
	switch tier {
	case TierPreCommit:
		// Hook must be reachable at pre-commit time. An empty stages list means
		// "default" (pre-commit), which is consistent. An explicit list must
		// include "pre-commit" or "default".
		if len(stages) == 0 {
			return true
		}
		for _, s := range stages {
			if s == "pre-commit" || s == "default" {
				return true
			}
		}
		return false
	case TierPrePush:
		// Hook must be reachable at pre-push time.
		if len(stages) == 0 {
			// Default stage is pre-commit only; not consistent with pre-push.
			return false
		}
		for _, s := range stages {
			if s == "pre-push" {
				return true
			}
		}
		return false
	default:
		// For pre-pr / ci-heavy / manual, hook_id should generally not be set;
		// if it is, we accept any stage rather than false-erroring.
		return true
	}
}

func checkGateHookIDs(hooks map[string]hookEntry, reg *Registry) []error {
	var errs []error
	for _, g := range reg.Gates {
		if g.HookID == "" {
			continue
		}
		he, ok := hooks[g.HookID]
		if !ok {
			errs = append(errs, fmt.Errorf(
				"drift: gate %q declares hook_id %q but that hook is not present in .pre-commit-config.yaml",
				g.ID, g.HookID,
			))
			continue
		}
		if !stageConsistentWithTier(he.Stages, g.Tier) {
			errs = append(errs, fmt.Errorf(
				"drift: gate %q (tier %s) hook_id %q has stages %v — inconsistent with gate tier "+
					"(pre-commit gate requires stage pre-commit/default; pre-push gate requires stage pre-push)",
				g.ID, g.Tier, g.HookID, he.Stages,
			))
		}
	}
	return errs
}
