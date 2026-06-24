// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// vulnScanScopeModeScoped is the default local one-shot scan mode: only
	// advisory and package-registry evidence covering the observed owned
	// packages must be present and fresh before the CLI declares a ready
	// answer.
	vulnScanScopeModeScoped = "scoped"
	// vulnScanScopeModeBroad opts the operator into broader advisory coverage
	// and skips the scoped stale-advisory guard. Package-registry metadata is
	// still required when observed package consumption needs it as join
	// evidence.
	vulnScanScopeModeBroad = "broad"
)

const (
	// vulnScanMissingAdvisoryCacheStale marks the scope plan when the readiness
	// envelope's aggregate freshness signal is `stale`. The envelope freshness
	// is the server-owned scoped verdict across evidence families and source
	// target state; the CLI does not reclassify individual source snapshots.
	vulnScanMissingAdvisoryCacheStale = "advisory_cache_stale"
	// vulnScanMissingAdvisoryCacheFreshnessUnknown marks a ready server verdict
	// that lacks a fresh aggregate cache signal. Unknown freshness is not a
	// clean result because the CLI cannot prove advisory or package evidence is
	// current for the scanned repository.
	vulnScanMissingAdvisoryCacheFreshnessUnknown = "advisory_cache_freshness_unknown"
	// vulnScanMissingPackageRegistryMetadata marks missing or stale
	// package-registry metadata required by the local vuln-scan scope.
	vulnScanMissingPackageRegistryMetadata = "package_registry_metadata"
)

// vulnScanScopePlan describes how the local vulnerability scan derived its
// target scope (observed dependency facts, advisory facts, package-registry
// facts) and which evidence gates the CLI applied before declaring a ready
// result. The plan is built from the readiness envelope returned by
// `/api/v0/supply-chain/impact/findings` so the local CLI never invents truth
// the server did not already report.
//
// Fact counts come from `evidence_sources[].fact_count` and reflect the raw
// number of source facts the readiness query observed, not the number of
// unique packages or advisory sources. `PackageRegistryFacts` may be 0 for a
// repository scope with no observed package consumption; once package
// consumption exists, scoped mode requires fresh package-registry metadata as
// join evidence.
type vulnScanScopePlan struct {
	Mode                     string                     `json:"mode"`
	ObservedDependencyFacts  int                        `json:"observed_dependency_facts"`
	AdvisoryFacts            int                        `json:"advisory_facts"`
	PackageRegistryFacts     int                        `json:"package_registry_facts"`
	PackageRegistryFreshness string                     `json:"package_registry_freshness,omitempty"`
	PackageRegistryComplete  bool                       `json:"package_registry_complete"`
	Freshness                string                     `json:"freshness,omitempty"`
	StopThreshold            string                     `json:"stop_threshold"`
	MissingEvidence          []string                   `json:"missing_evidence,omitempty"`
	IncompleteReasons        []string                   `json:"incomplete_reasons,omitempty"`
	SourceSnapshots          []vulnScanSourceCacheState `json:"source_snapshots,omitempty"`
}

// vulnScanSourceCacheState records the per-source cache health surfaced by the
// readiness envelope. The CLI presents this list for operator visibility while
// gating on the server's aggregate scoped freshness verdict.
type vulnScanSourceCacheState struct {
	Source               string `json:"source"`
	Ecosystem            string `json:"ecosystem,omitempty"`
	Freshness            string `json:"freshness,omitempty"`
	Complete             bool   `json:"complete"`
	CacheArtifactVersion string `json:"cache_artifact_version,omitempty"`
	WarningCode          string `json:"warning_code,omitempty"`
	WarningMessage       string `json:"warning_message,omitempty"`
}

// vulnScanPerformance records local one-shot scan performance evidence so the
// CLI output proves the bounded contract: repository size, observed
// dependency-fact count, advisory-fact count, wall-clock time, cache
// freshness, and the readiness state the scan stopped at. Fact counts mirror
// the same `evidence_sources[].fact_count` semantics as the scope plan.
type vulnScanPerformance struct {
	StartedAt                string `json:"started_at"`
	CompletedAt              string `json:"completed_at"`
	WallTimeMS               int64  `json:"wall_time_ms"`
	RepositorySizeBytes      int64  `json:"repository_size_bytes"`
	RepositoryFileCount      int    `json:"repository_file_count"`
	ObservedDependencyFacts  int    `json:"observed_dependency_facts"`
	AdvisoryFacts            int    `json:"advisory_facts"`
	PackageRegistryFacts     int    `json:"package_registry_facts"`
	PackageRegistryFreshness string `json:"package_registry_freshness,omitempty"`
	PackageRegistryComplete  bool   `json:"package_registry_complete"`
	CacheFreshness           string `json:"cache_freshness,omitempty"`
	ScopeMode                string `json:"scope_mode"`
	StopThreshold            string `json:"stop_threshold"`
}

// buildVulnScanScopePlan derives the scope-plan snapshot from the readiness
// envelope returned by the impact findings API. Mode is the caller-selected
// scope mode (scoped or broad); the plan is identical in either mode so
// operators can compare scoped vs broad runs.
func buildVulnScanScopePlan(mode string, readiness map[string]any) vulnScanScopePlan {
	plan := vulnScanScopePlan{Mode: mode}
	if readiness == nil {
		return plan
	}
	families := vulnScanReadinessEvidenceFamilies(readiness)
	for family, entry := range families {
		switch family {
		case "package.consumption":
			plan.ObservedDependencyFacts = entry.FactCount
		case "vulnerability.advisory":
			plan.AdvisoryFacts = entry.FactCount
		case "package.registry":
			plan.PackageRegistryFacts = entry.FactCount
			plan.PackageRegistryFreshness = entry.Freshness
		}
	}
	plan.PackageRegistryComplete = plan.PackageRegistryFacts > 0 &&
		strings.EqualFold(plan.PackageRegistryFreshness, "fresh")
	if plan.ObservedDependencyFacts > 0 && plan.PackageRegistryFacts == 0 &&
		strings.TrimSpace(plan.PackageRegistryFreshness) == "" {
		plan.PackageRegistryFreshness = "missing"
	}
	if freshness, ok := readiness["freshness"].(string); ok {
		plan.Freshness = strings.TrimSpace(freshness)
	}
	plan.SourceSnapshots = readinessSourceSnapshots(readiness)
	return plan
}

type vulnScanEvidenceFamilyState struct {
	FactCount int
	Freshness string
}

func vulnScanReadinessEvidenceFamilies(readiness map[string]any) map[string]vulnScanEvidenceFamilyState {
	states := map[string]vulnScanEvidenceFamilyState{}
	raw, ok := readiness["evidence_sources"].([]any)
	if !ok {
		return states
	}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		family, ok := entry["family"].(string)
		if !ok {
			continue
		}
		family = strings.TrimSpace(family)
		if family == "" {
			continue
		}
		state := states[family]
		switch typed := entry["fact_count"].(type) {
		case float64:
			state.FactCount = int(typed)
		case int:
			state.FactCount = typed
		}
		if freshness, ok := entry["freshness"].(string); ok {
			state.Freshness = strings.TrimSpace(freshness)
		}
		states[family] = state
	}
	return states
}

// readinessSourceSnapshots extracts a compact per-source cache view from the
// readiness envelope so the scope plan can show which advisory source caches
// triggered scoped guards.
func readinessSourceSnapshots(readiness map[string]any) []vulnScanSourceCacheState {
	raw, ok := readiness["source_snapshots"].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	snapshots := make([]vulnScanSourceCacheState, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		snap := vulnScanSourceCacheState{}
		if source, ok := entry["source"].(string); ok {
			snap.Source = strings.TrimSpace(source)
		}
		if ecosystem, ok := entry["ecosystem"].(string); ok {
			snap.Ecosystem = strings.TrimSpace(ecosystem)
		}
		if freshness, ok := entry["freshness"].(string); ok {
			snap.Freshness = strings.TrimSpace(freshness)
		}
		if complete, ok := entry["complete"].(bool); ok {
			snap.Complete = complete
		}
		if version, ok := entry["cache_artifact_version"].(string); ok {
			snap.CacheArtifactVersion = strings.TrimSpace(version)
		}
		if code, ok := entry["warning_code"].(string); ok {
			snap.WarningCode = strings.TrimSpace(code)
		}
		if message, ok := entry["warning_message"].(string); ok {
			snap.WarningMessage = strings.TrimSpace(message)
		}
		snapshots = append(snapshots, snap)
	}
	return snapshots
}

// applyScopedGuards inspects the scope plan and decides whether scoped mode
// should override the server-provided readiness state.
//
// Two CLI-side guards fire today. The scoped freshness guard downgrades
// `ready_*` answers when the envelope's aggregate `freshness` is not `fresh`,
// so the operator never gets a clean answer backed by stale source data or an
// unclassified freshness state. The package-registry guard then downgrades
// `ready_*` answers when the repository has observed dependency evidence but no
// fresh package metadata for those packages. The envelope freshness is the
// server-owned aggregate over scoped evidence families and source target state;
// per-source entries in `readiness.source_snapshots[]` stay visible in the
// plan, but the CLI does not reinterpret individual cache rows.
//
// Non-ready server verdicts (`not_configured`, `target_incomplete`,
// `evidence_incomplete`, `readiness_unavailable`) already preserve
// fail-closed semantics; the CLI passes them through unmodified rather than
// shadow the server's missing-evidence reasons.
//
// Broad mode short-circuits only the advisory freshness guard but still records
// the stop threshold so the JSON envelope is honest about the wider mode.
func applyScopedGuards(
	plan *vulnScanScopePlan,
	readinessState string,
) (newState string, missing []string, failErr error) {
	if plan == nil {
		return readinessState, nil, nil
	}
	if plan.Mode == vulnScanScopeModeBroad {
		plan.StopThreshold = readinessState
		if missing := packageRegistryMissingEvidence(plan, readinessState); len(missing) > 0 {
			return failClosedVulnScanScope(plan, missing)
		}
		return readinessState, nil, nil
	}
	plan.StopThreshold = readinessState
	if !isReadyReadinessState(readinessState) {
		return readinessState, nil, nil
	}
	if missing := packageRegistryMissingEvidence(plan, readinessState); len(missing) > 0 {
		return failClosedVulnScanScope(plan, missing)
	}
	freshness := strings.ToLower(strings.TrimSpace(plan.Freshness))
	if freshness == "fresh" {
		return readinessState, nil, nil
	}
	if freshness == "stale" {
		return failClosedVulnScanScope(plan, []string{vulnScanMissingAdvisoryCacheStale})
	}
	return failClosedVulnScanScope(plan, []string{vulnScanMissingAdvisoryCacheFreshnessUnknown})
}

func packageRegistryMissingEvidence(plan *vulnScanScopePlan, readinessState string) []string {
	if plan == nil || !isReadyReadinessState(readinessState) {
		return nil
	}
	if plan.ObservedDependencyFacts == 0 {
		return nil
	}
	if plan.PackageRegistryFacts == 0 ||
		!strings.EqualFold(plan.PackageRegistryFreshness, "fresh") ||
		!plan.PackageRegistryComplete {
		return []string{vulnScanMissingPackageRegistryMetadata}
	}
	return nil
}

func failClosedVulnScanScope(
	plan *vulnScanScopePlan,
	missing []string,
) (newState string, outMissing []string, failErr error) {
	state := "evidence_incomplete"
	plan.MissingEvidence = missing
	plan.StopThreshold = state
	failErr = commandExitError{
		message: fmt.Sprintf("vuln-scan fail-closed: %s", strings.Join(missing, ", ")),
		code:    4,
	}
	return state, missing, failErr
}

// isReadyReadinessState reports whether a readiness state classifies the
// scope as ready (zero findings or with findings). Used by scoped guards to
// decide whether the CLI should override the server's verdict.
func isReadyReadinessState(state string) bool {
	switch strings.TrimSpace(state) {
	case "ready_zero_findings", "ready_with_findings":
		return true
	default:
		return false
	}
}

// resolveScopeMode returns the canonical scope mode for the CLI given the
// --broad flag. It centralizes the default so future modes (e.g. a future
// --scope=narrow|broad) stay consistent across output paths.
func resolveScopeMode(broad bool) string {
	if broad {
		return vulnScanScopeModeBroad
	}
	return vulnScanScopeModeScoped
}

// captureVulnScanPerformance builds the scan_performance block written to the
// JSON envelope. Wall time uses the same wall clock the CLI used to record
// scan start; repository size is best-effort via filesystem walk so a missing
// path is treated as zero rather than aborting the report.
func captureVulnScanPerformance(
	startedAt time.Time,
	completedAt time.Time,
	plan vulnScanScopePlan,
	repoRoot string,
) vulnScanPerformance {
	bytes, count := measureRepositoryFootprint(repoRoot)
	freshness := plan.Freshness
	if freshness == "" {
		freshness = "unknown"
	}
	return vulnScanPerformance{
		StartedAt:                startedAt.UTC().Format(time.RFC3339Nano),
		CompletedAt:              completedAt.UTC().Format(time.RFC3339Nano),
		WallTimeMS:               completedAt.Sub(startedAt).Milliseconds(),
		RepositorySizeBytes:      bytes,
		RepositoryFileCount:      count,
		ObservedDependencyFacts:  plan.ObservedDependencyFacts,
		AdvisoryFacts:            plan.AdvisoryFacts,
		PackageRegistryFacts:     plan.PackageRegistryFacts,
		PackageRegistryFreshness: plan.PackageRegistryFreshness,
		PackageRegistryComplete:  plan.PackageRegistryComplete,
		CacheFreshness:           freshness,
		ScopeMode:                plan.Mode,
		StopThreshold:            plan.StopThreshold,
	}
}

// measureRepositoryFootprint walks the repository root once and returns the
// total bytes and file count. It is bounded by the scanned path and skips
// errors so a transient filesystem issue cannot fail the CLI report. The
// caller treats this as performance evidence only, not as truth input.
func measureRepositoryFootprint(root string) (int64, int) {
	if strings.TrimSpace(root) == "" {
		return 0, 0
	}
	info, err := os.Stat(root)
	if err != nil {
		return 0, 0
	}
	if !info.IsDir() {
		return info.Size(), 1
	}
	var totalBytes int64
	var totalFiles int
	_ = filepath.WalkDir(root, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		totalBytes += info.Size()
		totalFiles++
		return nil
	})
	return totalBytes, totalFiles
}
