// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// ScopeModeScoped is the default local one-shot scan mode: only advisory
	// and package-registry evidence covering the observed owned packages must
	// be present and fresh before the CLI declares a ready answer.
	ScopeModeScoped = "scoped"
	// ScopeModeBroad opts the operator into broader advisory coverage and
	// skips the scoped stale-advisory guard. Package-registry metadata is
	// still required when observed package consumption needs it as join
	// evidence.
	ScopeModeBroad = "broad"
)

const (
	// MissingAdvisoryCacheStale marks the scope plan when the readiness
	// envelope's aggregate freshness signal is `stale`. The envelope freshness
	// is the server-owned scoped verdict across evidence families and source
	// target state; the CLI does not reclassify individual source snapshots.
	MissingAdvisoryCacheStale = "advisory_cache_stale"
	// MissingAdvisoryCacheFreshnessUnknown marks a ready server verdict that
	// lacks a fresh aggregate cache signal. Unknown freshness is not a clean
	// result because the CLI cannot prove advisory or package evidence is
	// current for the scanned repository.
	MissingAdvisoryCacheFreshnessUnknown = "advisory_cache_freshness_unknown"
	// MissingPackageRegistryMetadata marks missing or stale package-registry
	// metadata required by the local vuln-scan scope.
	MissingPackageRegistryMetadata = "package_registry_metadata"
)

// ScopePlan describes how the local vulnerability scan derived its target
// scope (observed dependency facts, advisory facts, package-registry facts)
// and which evidence gates the CLI applied before declaring a ready result.
// The plan is built from the readiness envelope returned by
// `/api/v0/supply-chain/impact/findings` so the local CLI never invents truth
// the server did not already report.
//
// Fact counts come from `evidence_sources[].fact_count` and reflect the raw
// number of source facts the readiness query observed, not the number of
// unique packages or advisory sources. PackageRegistryFacts may be 0 for a
// repository scope with no observed package consumption; once package
// consumption exists, scoped mode requires fresh package-registry metadata as
// join evidence.
type ScopePlan struct {
	Mode                     string             `json:"mode"`
	ObservedDependencyFacts  int                `json:"observed_dependency_facts"`
	AdvisoryFacts            int                `json:"advisory_facts"`
	PackageRegistryFacts     int                `json:"package_registry_facts"`
	PackageRegistryFreshness string             `json:"package_registry_freshness,omitempty"`
	PackageRegistryComplete  bool               `json:"package_registry_complete"`
	Freshness                string             `json:"freshness,omitempty"`
	StopThreshold            string             `json:"stop_threshold"`
	MissingEvidence          []string           `json:"missing_evidence,omitempty"`
	IncompleteReasons        []string           `json:"incomplete_reasons,omitempty"`
	SourceSnapshots          []SourceCacheState `json:"source_snapshots,omitempty"`
}

// SourceCacheState records the per-source cache health surfaced by the
// readiness envelope. The CLI presents this list for operator visibility while
// gating on the server's aggregate scoped freshness verdict.
type SourceCacheState struct {
	Source               string `json:"source"`
	Ecosystem            string `json:"ecosystem,omitempty"`
	Freshness            string `json:"freshness,omitempty"`
	Complete             bool   `json:"complete"`
	CacheArtifactVersion string `json:"cache_artifact_version,omitempty"`
	WarningCode          string `json:"warning_code,omitempty"`
	WarningMessage       string `json:"warning_message,omitempty"`
}

// Performance records local one-shot scan performance evidence so the CLI
// output proves the bounded contract: repository size, observed
// dependency-fact count, advisory-fact count, wall-clock time, cache
// freshness, and the readiness state the scan stopped at. Fact counts mirror
// the same `evidence_sources[].fact_count` semantics as the scope plan.
type Performance struct {
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

// BuildScopePlan derives the scope-plan snapshot from the readiness envelope
// returned by the impact findings API. mode is the caller-selected scope mode
// (scoped or broad); the plan is identical in either mode so operators can
// compare scoped and broad runs.
func BuildScopePlan(mode string, readiness map[string]any) ScopePlan {
	plan := ScopePlan{Mode: mode}
	if readiness == nil {
		return plan
	}
	families := readinessEvidenceFamilyStates(readiness)
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

// evidenceFamilyState is the per-family slice of the readiness envelope the
// scope plan needs: how many source facts the readiness query observed, and
// how fresh that family's evidence is.
type evidenceFamilyState struct {
	FactCount int
	Freshness string
}

// readinessEvidenceFamilyStates indexes `readiness.evidence_sources[]` by
// family name. Entries without a family name are skipped rather than merged
// under an empty key, so a malformed row cannot silently satisfy a guard.
func readinessEvidenceFamilyStates(readiness map[string]any) map[string]evidenceFamilyState {
	states := map[string]evidenceFamilyState{}
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
func readinessSourceSnapshots(readiness map[string]any) []SourceCacheState {
	raw, ok := readiness["source_snapshots"].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	snapshots := make([]SourceCacheState, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		snap := SourceCacheState{}
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

// ApplyScopedGuards inspects the scope plan and decides whether scoped mode
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
// `evidence_incomplete`, `readiness_unavailable`) already preserve fail-closed
// semantics; the CLI passes them through unmodified rather than shadow the
// server's missing-evidence reasons.
//
// Broad mode short-circuits only the advisory freshness guard but still records
// the stop threshold so the JSON envelope is honest about the wider mode.
func ApplyScopedGuards(
	plan *ScopePlan,
	readinessState string,
) (newState string, missing []string, failure *Failure) {
	if plan == nil {
		return readinessState, nil, nil
	}
	if plan.Mode == ScopeModeBroad {
		plan.StopThreshold = readinessState
		if missing := PackageRegistryMissingEvidence(plan, readinessState); len(missing) > 0 {
			return failClosedScope(plan, missing)
		}
		return readinessState, nil, nil
	}
	plan.StopThreshold = readinessState
	if !IsReadyReadinessState(readinessState) {
		return readinessState, nil, nil
	}
	if missing := PackageRegistryMissingEvidence(plan, readinessState); len(missing) > 0 {
		return failClosedScope(plan, missing)
	}
	freshness := strings.ToLower(strings.TrimSpace(plan.Freshness))
	if freshness == "fresh" {
		return readinessState, nil, nil
	}
	if freshness == "stale" {
		return failClosedScope(plan, []string{MissingAdvisoryCacheStale})
	}
	return failClosedScope(plan, []string{MissingAdvisoryCacheFreshnessUnknown})
}

// PackageRegistryMissingEvidence reports the package-registry evidence a ready
// verdict is missing. A repository with no observed dependency facts needs
// none; once dependencies are observed, the registry metadata must be present,
// fresh, and complete before the answer counts as clean.
func PackageRegistryMissingEvidence(plan *ScopePlan, readinessState string) []string {
	if plan == nil || !IsReadyReadinessState(readinessState) {
		return nil
	}
	if plan.ObservedDependencyFacts == 0 {
		return nil
	}
	if plan.PackageRegistryFacts == 0 ||
		!strings.EqualFold(plan.PackageRegistryFreshness, "fresh") ||
		!plan.PackageRegistryComplete {
		return []string{MissingPackageRegistryMetadata}
	}
	return nil
}

// failClosedScope records the missing evidence on the plan and downgrades the
// run to `evidence_incomplete` with exit code 4.
func failClosedScope(
	plan *ScopePlan,
	missing []string,
) (newState string, outMissing []string, failure *Failure) {
	state := "evidence_incomplete"
	plan.MissingEvidence = missing
	plan.StopThreshold = state
	failure = &Failure{
		Message: fmt.Sprintf("vuln-scan fail-closed: %s", strings.Join(missing, ", ")),
		Code:    4,
	}
	return state, missing, failure
}

// IsReadyReadinessState reports whether a readiness state classifies the scope
// as ready (zero findings or with findings). The scoped guards use it to
// decide whether the CLI should override the server's verdict.
func IsReadyReadinessState(state string) bool {
	switch strings.TrimSpace(state) {
	case "ready_zero_findings", "ready_with_findings":
		return true
	default:
		return false
	}
}

// ResolveScopeMode returns the canonical scope mode for the CLI given the
// --broad flag. It centralizes the default so future modes (for example a
// --scope=narrow|broad) stay consistent across output paths.
func ResolveScopeMode(broad bool) string {
	if broad {
		return ScopeModeBroad
	}
	return ScopeModeScoped
}

// CapturePerformance builds the scan_performance block written to the JSON
// envelope. Wall time uses the same wall clock the CLI used to record scan
// start; repository size is best-effort via filesystem walk, so a missing path
// is treated as zero rather than aborting the report.
func CapturePerformance(
	startedAt time.Time,
	completedAt time.Time,
	plan ScopePlan,
	repoRoot string,
) Performance {
	bytes, count := measureRepositoryFootprint(repoRoot)
	freshness := plan.Freshness
	if freshness == "" {
		freshness = "unknown"
	}
	return Performance{
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
