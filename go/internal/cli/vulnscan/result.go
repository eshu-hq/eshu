// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ImpactFindingsEndpoint is the API route `eshu vuln-scan repo` reads its
// findings from. It is exported because the envelope reports it back to the
// operator as evidence, and because the provider-parity command reads the same
// route.
const ImpactFindingsEndpoint = "/api/v0/supply-chain/impact/findings"

// Now is the clock every timestamp in the report and the performance block is
// stamped from. It is a variable so a test can pin wall-clock time instead of
// racing the live scanner clock; production callers leave it as time.Now.
var Now = time.Now

// EnvelopeFetcher is the transport this family needs: one GET that decodes a
// canonical Eshu envelope into result. It is declared here, where it is
// consumed, because the concrete client lives in go/cmd/eshu, which is package
// main and cannot be imported.
type EnvelopeFetcher interface {
	GetEnvelope(path string, result any) error
}

// Target is the repository the scan ran against, as the vuln-scan envelope and
// report carry it. The `eshu scan` family owns the equivalent type in package
// main and resolves the values; this is the report's copy of the same three
// fields, with the same JSON names.
type Target struct {
	Path string `json:"path"`
	Root string `json:"root"`
	Kind string `json:"kind"`
}

// Result is the `data` member of the `eshu vuln-scan repo` JSON envelope, and
// the single value the report builder, the SARIF and VEX writers, the scope
// guards, and the human renderer all read.
type Result struct {
	Command        string `json:"command"`
	Status         string `json:"status"`
	ReadinessState string `json:"readiness_state"`
	ScopeMode      string `json:"scope_mode"`
	Target         Target `json:"target"`
	RepositoryID   string `json:"repository_id,omitempty"`
	// Scan is the `eshu scan` result for the same run. It is typed any because
	// that type belongs to the scan family in package main: this package
	// carries the value into the envelope and never reads it. The command
	// wrapper assigns it on every path that writes an envelope.
	Scan        any              `json:"scan"`
	Findings    []map[string]any `json:"findings"`
	Count       int              `json:"count"`
	Limit       int              `json:"limit"`
	Truncated   bool             `json:"truncated"`
	NextCursor  map[string]any   `json:"next_cursor,omitempty"`
	Readiness   map[string]any   `json:"readiness,omitempty"`
	ScopePlan   *ScopePlan       `json:"scope_plan,omitempty"`
	Performance *Performance     `json:"scan_performance,omitempty"`
	Report      *Report          `json:"report,omitempty"`
	Warnings    []string         `json:"warnings,omitempty"`
	Evidence    RepoEvidence     `json:"evidence"`
}

// RepoEvidence names where the findings came from, so an operator reading the
// envelope can re-run the same read by hand.
type RepoEvidence struct {
	ServiceURL       string `json:"service_url"`
	FindingsEndpoint string `json:"findings_endpoint"`
}

// RepoError is the envelope's error member. The command wrapper fills it from
// the error it is about to exit with.
type RepoError struct {
	Message string `json:"message"`
}

// ImpactFindingsEnvelope is the impact-findings response as this family
// consumes it.
type ImpactFindingsEnvelope struct {
	Data  ImpactFindingsData `json:"data"`
	Truth map[string]any     `json:"truth"`
	Error *RepoError         `json:"error"`
}

// ImpactFindingsData is the `data` member of the impact-findings response.
// Findings stay generic maps: the CLI names a fixed set of keys when it builds
// the report and passes the rest through to --json untouched, so a server-side
// field addition reaches operators without a CLI release.
type ImpactFindingsData struct {
	Findings   []map[string]any `json:"findings"`
	Count      int              `json:"count"`
	Limit      int              `json:"limit"`
	Truncated  bool             `json:"truncated"`
	NextCursor map[string]any   `json:"next_cursor,omitempty"`
	Readiness  map[string]any   `json:"readiness,omitempty"`
}

// NewResult builds the pre-scan Result. It starts at the most pessimistic
// state — status `failed`, readiness `target_incomplete` — so an early return
// on any path still writes a fail-closed envelope rather than an empty one.
func NewResult(target Target, limit int, broad bool, serviceURL string) Result {
	return Result{
		Command:        "vuln-scan repo",
		Status:         "failed",
		ReadinessState: "target_incomplete",
		ScopeMode:      ResolveScopeMode(broad),
		Target:         target,
		Findings:       []map[string]any{},
		Limit:          limit,
		Evidence: RepoEvidence{
			ServiceURL:       serviceURL,
			FindingsEndpoint: ImpactFindingsEndpoint,
		},
	}
}

// FetchImpactFindings reads the reducer-owned impact findings for
// repositoryID. impactStatus is optional and filters server-side when set.
func FetchImpactFindings(
	client EnvelopeFetcher,
	repositoryID string,
	limit int,
	impactStatus string,
) (ImpactFindingsEnvelope, error) {
	if client == nil {
		return ImpactFindingsEnvelope{}, fmt.Errorf("missing API client")
	}
	repositoryID = strings.TrimSpace(repositoryID)
	if repositoryID == "" {
		return ImpactFindingsEnvelope{}, fmt.Errorf("repository id is required")
	}
	query := url.Values{}
	query.Set("repository_id", repositoryID)
	query.Set("limit", fmt.Sprintf("%d", limit))
	if strings.TrimSpace(impactStatus) != "" {
		query.Set("impact_status", strings.TrimSpace(impactStatus))
	}
	var envelope ImpactFindingsEnvelope
	path := ImpactFindingsEndpoint + "?" + query.Encode()
	if err := client.GetEnvelope(path, &envelope); err != nil {
		return ImpactFindingsEnvelope{}, fmt.Errorf("fetch vulnerability impact findings: %w", err)
	}
	return envelope, nil
}

// ReadinessState prefers the server-side readiness verdict so the CLI surfaces
// not_configured, target_incomplete, evidence_incomplete, unsupported, or
// readiness_unavailable when the server reports them. It only falls back to the
// count-based heuristic when the response carries no readiness envelope (older
// API versions).
func ReadinessState(readiness map[string]any, count int) string {
	if state, ok := readiness["readiness_state"].(string); ok && strings.TrimSpace(state) != "" {
		return strings.TrimSpace(state)
	}
	if count > 0 {
		return "ready_with_findings"
	}
	return "ready_zero_findings"
}

// ApplyScope builds the scope plan from the readiness envelope, runs the
// fail-closed guards, and surfaces the broad-mode note when the operator opted
// into wider advisory coverage. It returns the fail-closed Failure so the
// caller can short-circuit the success path while still emitting the JSON
// envelope with the scope plan attached.
func ApplyScope(result *Result) *Failure {
	plan := BuildScopePlan(result.ScopeMode, result.Readiness)
	state, missing, failure := ApplyScopedGuards(&plan, result.ReadinessState)
	if plan.StopThreshold == "" {
		plan.StopThreshold = state
	}
	result.ScopePlan = &plan

	if plan.Mode == ScopeModeBroad {
		if failure != nil {
			result.ReadinessState = state
			if len(missing) > 0 {
				result.Warnings = append(
					result.Warnings,
					fmt.Sprintf("broad mode fail-closed: %s", strings.Join(missing, ", ")),
				)
			}
			return failure
		}
		result.Warnings = append(
			result.Warnings,
			"broad mode skipped advisory freshness fail-closed guard; package-registry metadata still must be fresh when observed dependencies require it",
		)
		return nil
	}
	if failure == nil {
		return nil
	}
	result.ReadinessState = state
	if len(missing) > 0 {
		result.Warnings = append(
			result.Warnings,
			fmt.Sprintf("vuln-scan fail-closed: %s", strings.Join(missing, ", ")),
		)
	}
	return failure
}

// RecordPerformance stamps the scan_performance block onto result at the very
// end of the run so wall time covers the whole orchestrated flow — bootstrap,
// readiness wait, findings read, scope guards — rather than a single stage.
// It is safe to call more than once; the most recent call wins.
func RecordPerformance(result *Result, startedAt time.Time, repoRoot string) {
	plan := ScopePlan{Mode: result.ScopeMode, StopThreshold: result.ReadinessState}
	if result.ScopePlan != nil {
		plan = *result.ScopePlan
		if plan.StopThreshold == "" {
			plan.StopThreshold = result.ReadinessState
		}
	}
	perf := CapturePerformance(startedAt, Now(), plan, repoRoot)
	result.Performance = &perf
}
