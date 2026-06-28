// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

const budgetProofSchemaVersion = "capability-budget-proof/v1"

var budgetPrivateDataPattern = regexp.MustCompile(`(?i)(ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|https?://|arn:(aws|aws-us-gov|aws-cn):|/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/|(^|[^0-9])[0-9]{12}([^0-9]|$)|([0-9]{1,3}\.){3}[0-9]{1,3}|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,})`)

// BudgetProofArtifact is the public-safe per-capability performance-budget
// proof artifact checked against the capability matrix.
type BudgetProofArtifact struct {
	SchemaVersion string              `json:"schema_version"`
	Status        string              `json:"status"`
	Run           BudgetProofRun      `json:"run"`
	Measurements  []BudgetMeasurement `json:"measurements"`
}

// BudgetProofRun records artifact-wide run metadata.
type BudgetProofRun struct {
	Issue   int                `json:"issue"`
	Commit  string             `json:"commit"`
	Backend BudgetProofBackend `json:"backend"`
}

// BudgetProofBackend records the graph backend used by a measurement.
type BudgetProofBackend struct {
	Kind    string `json:"kind"`
	Version string `json:"version"`
}

// BudgetMeasurement binds one capability/profile budget row to measured proof.
type BudgetMeasurement struct {
	Capability     string              `json:"capability"`
	Profile        string              `json:"profile"`
	APIRoutes      []string            `json:"api_routes,omitempty"`
	MCPTools       []string            `json:"mcp_tools,omitempty"`
	CorpusSlot     string              `json:"corpus_slot"`
	Backend        BudgetProofBackend  `json:"backend"`
	Latency        BudgetLatency       `json:"latency"`
	Scope          BudgetScopeProof    `json:"scope"`
	ArtifactHandle string              `json:"artifact_handle"`
	Commit         string              `json:"commit"`
	Freshness      BudgetFreshness     `json:"freshness"`
	SurfaceParity  BudgetSurfaceParity `json:"surface_parity,omitempty"`
	RetryCount     int                 `json:"retry_count"`
	DeadLetters    int                 `json:"dead_letter_count"`
	Status         string              `json:"status"`
	LinkedIssue    int                 `json:"linked_issue,omitempty"`
}

// BudgetLatency records measured latency percentiles in milliseconds.
type BudgetLatency struct {
	P50MS int    `json:"p50_ms"`
	P95MS int    `json:"p95_ms"`
	P99MS int    `json:"p99_ms"`
	NA    string `json:"not_applicable_reason,omitempty"`
}

// BudgetScopeProof records max-scope and truncation proof for a measurement.
type BudgetScopeProof struct {
	DeclaredMaxScopeSize string `json:"declared_max_scope_size"`
	ResultScope          string `json:"result_scope"`
	LimitEnforced        bool   `json:"limit_enforced"`
	TruncationProof      string `json:"truncation_proof"`
	TruncationInvariant  string `json:"truncation_invariant"`
}

// BudgetFreshness records the measurement freshness window.
type BudgetFreshness struct {
	MeasuredAt string `json:"measured_at"`
	ExpiresAt  string `json:"expires_at"`
}

// BudgetSurfaceParity records API/MCP agreement for proxied surfaces.
type BudgetSurfaceParity struct {
	Status      string `json:"status,omitempty"`
	APIP95MS    int    `json:"api_p95_ms,omitempty"`
	MCPP95MS    int    `json:"mcp_p95_ms,omitempty"`
	MaxDeltaMS  int    `json:"max_delta_ms,omitempty"`
	ComparedBy  string `json:"compared_by,omitempty"`
	ProofHandle string `json:"proof_handle,omitempty"`
}

// BudgetFindingKind classifies a capability-budget proof finding.
type BudgetFindingKind string

const (
	// BudgetFindingInvalidArtifact means artifact root metadata is malformed.
	BudgetFindingInvalidArtifact BudgetFindingKind = "invalid_artifact"
	// BudgetFindingMissingMeasurement means a supported budget row has no proof.
	BudgetFindingMissingMeasurement BudgetFindingKind = "missing_measurement"
	// BudgetFindingP95OverBudget means measured p95 exceeds the declared budget.
	BudgetFindingP95OverBudget BudgetFindingKind = "p95_over_budget"
	// BudgetFindingScopeNotProven means max-scope or truncation proof is absent.
	BudgetFindingScopeNotProven BudgetFindingKind = "scope_not_proven"
	// BudgetFindingSurfaceParityFailed means API and MCP proof disagree.
	BudgetFindingSurfaceParityFailed BudgetFindingKind = "surface_parity_failed"
	// BudgetFindingRuntimeInvariantFailed means pass status hides runtime faults.
	BudgetFindingRuntimeInvariantFailed BudgetFindingKind = "runtime_invariant_failed"
	// BudgetFindingPrivateData means the public artifact contains private data.
	BudgetFindingPrivateData BudgetFindingKind = "private_data"
)

// BudgetFinding is one failed performance-budget proof invariant.
type BudgetFinding struct {
	Kind    BudgetFindingKind `json:"kind"`
	Subject string            `json:"subject"`
	Detail  string            `json:"detail"`
}

// LoadBudgetProofArtifact reads a public-safe capability budget proof artifact
// from disk.
func LoadBudgetProofArtifact(path string) (BudgetProofArtifact, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- caller supplies an operator-selected proof artifact path
	if err != nil {
		return BudgetProofArtifact{}, fmt.Errorf("read capability budget proof artifact %s: %w", path, err)
	}
	var artifact BudgetProofArtifact
	if err := json.Unmarshal(raw, &artifact); err != nil {
		return BudgetProofArtifact{}, fmt.Errorf("parse capability budget proof artifact %s: %w", path, err)
	}
	return artifact, nil
}

// CheckBudgetProof verifies that every supported matrix budget row is bound to
// a public-safe measured proof artifact.
func CheckBudgetProof(matrix Matrix, artifact BudgetProofArtifact) []BudgetFinding {
	var findings []BudgetFinding
	findings = append(findings, artifactShapeFindings(artifact)...)
	if publicArtifactContainsPrivateData(artifact) {
		findings = append(findings, BudgetFinding{
			Kind:    BudgetFindingPrivateData,
			Subject: "artifact",
			Detail:  "public capability budget proof artifact contains private-looking data",
		})
	}

	byRow := map[string]BudgetMeasurement{}
	for _, measurement := range artifact.Measurements {
		byRow[budgetRowKey(measurement.Capability, measurement.Profile)] = measurement
		findings = append(findings, measurementFindings(artifact.Status, measurement)...)
	}

	for _, capability := range matrix.Capabilities {
		for profileID, profile := range capability.Profiles {
			if !requiresBudgetProof(profile) {
				continue
			}
			measurement, ok := byRow[budgetRowKey(capability.Capability, profileID)]
			if !ok {
				findings = append(findings, BudgetFinding{
					Kind:    BudgetFindingMissingMeasurement,
					Subject: budgetRowKey(capability.Capability, profileID),
					Detail:  "supported capability profile declares p95_latency_ms or max_scope_size without measured proof",
				})
				continue
			}
			findings = append(findings, measurementBudgetFindings(capability.Capability, profileID, profile, measurement)...)
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		return findings[i].Subject < findings[j].Subject
	})
	return findings
}

func artifactShapeFindings(artifact BudgetProofArtifact) []BudgetFinding {
	var findings []BudgetFinding
	if artifact.SchemaVersion != budgetProofSchemaVersion {
		findings = append(findings, invalidBudgetArtifact("schema_version", "schema_version must be capability-budget-proof/v1"))
	}
	if artifact.Status != "pass" && artifact.Status != "partial" && artifact.Status != "fail" {
		findings = append(findings, invalidBudgetArtifact("status", "status must be pass, partial, or fail"))
	}
	if artifact.Run.Issue != 4062 {
		findings = append(findings, invalidBudgetArtifact("run.issue", "run.issue must be 4062"))
	}
	if !isCommitSHA(artifact.Run.Commit) {
		findings = append(findings, invalidBudgetArtifact("run.commit", "run.commit must be a 40-character lowercase commit SHA"))
	}
	if artifact.Run.Backend.Kind != "nornicdb" && artifact.Run.Backend.Kind != "neo4j" {
		findings = append(findings, invalidBudgetArtifact("run.backend.kind", "backend kind must be nornicdb or neo4j"))
	}
	if strings.TrimSpace(artifact.Run.Backend.Version) == "" {
		findings = append(findings, invalidBudgetArtifact("run.backend.version", "backend version is required"))
	}
	return findings
}

func invalidBudgetArtifact(subject, detail string) BudgetFinding {
	return BudgetFinding{Kind: BudgetFindingInvalidArtifact, Subject: subject, Detail: detail}
}

func measurementFindings(artifactStatus string, measurement BudgetMeasurement) []BudgetFinding {
	subject := budgetRowKey(measurement.Capability, measurement.Profile)
	var findings []BudgetFinding
	if artifactStatus == "pass" && measurement.Status != "pass" {
		findings = append(findings, BudgetFinding{
			Kind:    BudgetFindingRuntimeInvariantFailed,
			Subject: subject,
			Detail:  "pass artifact requires every measurement row status to pass",
		})
	}
	if measurement.Status == "pass" && (measurement.RetryCount != 0 || measurement.DeadLetters != 0 || measurement.Scope.TruncationInvariant != "pass") {
		findings = append(findings, BudgetFinding{
			Kind:    BudgetFindingRuntimeInvariantFailed,
			Subject: subject,
			Detail:  "pass measurement requires zero retries, zero dead letters, and passing truncation invariant",
		})
	}
	findings = append(findings, surfaceParityFindings(subject, measurement)...)
	if measurement.ArtifactHandle == "" || measurement.CorpusSlot == "" || measurement.Commit == "" {
		findings = append(findings, invalidBudgetArtifact(subject, "measurement requires artifact handle, corpus slot, and commit"))
	}
	if measurement.Commit != "" && !isCommitSHA(measurement.Commit) {
		findings = append(findings, invalidBudgetArtifact(subject, "measurement commit must be a 40-character lowercase commit SHA"))
	}
	if measurement.Freshness.MeasuredAt == "" || measurement.Freshness.ExpiresAt == "" {
		findings = append(findings, invalidBudgetArtifact(subject, "measurement freshness requires measured_at and expires_at"))
	}
	return findings
}

func surfaceParityFindings(subject string, measurement BudgetMeasurement) []BudgetFinding {
	if len(measurement.APIRoutes) == 0 || len(measurement.MCPTools) == 0 {
		return nil
	}
	parity := measurement.SurfaceParity
	if parity.Status != "pass" {
		return []BudgetFinding{{
			Kind:    BudgetFindingSurfaceParityFailed,
			Subject: subject,
			Detail:  "API and MCP proxied measurement requires surface_parity.status=pass",
		}}
	}
	if parity.APIP95MS <= 0 || parity.MCPP95MS <= 0 || parity.MaxDeltaMS <= 0 || strings.TrimSpace(parity.ProofHandle) == "" {
		return []BudgetFinding{{
			Kind:    BudgetFindingSurfaceParityFailed,
			Subject: subject,
			Detail:  "API and MCP proxied measurement requires positive API/MCP p95 values, max delta, and proof handle",
		}}
	}
	if absInt(parity.APIP95MS-parity.MCPP95MS) > parity.MaxDeltaMS {
		return []BudgetFinding{{
			Kind:    BudgetFindingSurfaceParityFailed,
			Subject: subject,
			Detail:  fmt.Sprintf("API p95 %dms and MCP p95 %dms differ by more than %dms", parity.APIP95MS, parity.MCPP95MS, parity.MaxDeltaMS),
		}}
	}
	return nil
}

func measurementBudgetFindings(capability, profileID string, profile MatrixProfile, measurement BudgetMeasurement) []BudgetFinding {
	subject := budgetRowKey(capability, profileID)
	var findings []BudgetFinding
	if profile.P95LatencyMS != nil {
		if measurement.Latency.NA != "" || measurement.Latency.P50MS <= 0 || measurement.Latency.P95MS <= 0 || measurement.Latency.P99MS <= 0 {
			findings = append(findings, BudgetFinding{
				Kind:    BudgetFindingMissingMeasurement,
				Subject: subject,
				Detail:  "p95 budget rows require measured p50, p95, and p99 latency percentiles",
			})
		}
		if measurement.Latency.P95MS > *profile.P95LatencyMS && measurement.LinkedIssue == 0 {
			findings = append(findings, BudgetFinding{
				Kind:    BudgetFindingP95OverBudget,
				Subject: subject,
				Detail:  fmt.Sprintf("measured p95 %dms exceeds declared budget %dms without linked issue", measurement.Latency.P95MS, *profile.P95LatencyMS),
			})
		}
	}
	if profile.MaxScopeSize != "" && profile.MaxScopeSize != "none" {
		if measurement.Scope.DeclaredMaxScopeSize != profile.MaxScopeSize || !measurement.Scope.LimitEnforced || measurement.Scope.TruncationProof == "" {
			findings = append(findings, BudgetFinding{
				Kind:    BudgetFindingScopeNotProven,
				Subject: subject,
				Detail:  "measurement must prove declared max scope, limit enforcement, and truncation behavior",
			})
		}
	}
	return findings
}

func requiresBudgetProof(profile MatrixProfile) bool {
	status := effectiveStatus(profile)
	if status != statusSupported && status != statusExperimental {
		return false
	}
	return profile.P95LatencyMS != nil || (profile.MaxScopeSize != "" && profile.MaxScopeSize != "none")
}

func publicArtifactContainsPrivateData(artifact BudgetProofArtifact) bool {
	redacted := artifact
	redacted.Run.Commit = ""
	redacted.Measurements = append([]BudgetMeasurement(nil), artifact.Measurements...)
	for i := range redacted.Measurements {
		redacted.Measurements[i].Commit = ""
	}
	payload, err := json.Marshal(redacted)
	if err != nil {
		return true
	}
	return budgetPrivateDataPattern.Match(payload)
}

func budgetRowKey(capability, profile string) string {
	return capability + "/" + profile
}

func isCommitSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
