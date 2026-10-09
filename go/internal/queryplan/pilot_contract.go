// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

// PilotContract is the executable methodology attached to an existing query
// manifest entry. Version 1 is intentionally closed; new semantics need a new
// version and validator. No database access occurs in this package.
type PilotContract struct {
	Version               int                   `yaml:"version" json:"version"`
	Result                string                `yaml:"result" json:"result"`
	Authorization         string                `yaml:"authorization" json:"authorization"`
	CurrentHistory        string                `yaml:"current_history" json:"current_history"`
	Nulls                 string                `yaml:"nulls" json:"nulls"`
	Duplicates            string                `yaml:"duplicates" json:"duplicates"`
	Ordering              string                `yaml:"ordering" json:"ordering"`
	Pagination            string                `yaml:"pagination" json:"pagination"`
	Partial               string                `yaml:"partial" json:"partial"`
	Patterns              []string              `yaml:"patterns" json:"patterns"`
	Rationale             string                `yaml:"rationale" json:"rationale"`
	Alternatives          []string              `yaml:"alternatives" json:"alternatives"`
	IndexCost             string                `yaml:"index_cost" json:"index_cost"`
	Exceptions            []string              `yaml:"exceptions" json:"exceptions"`
	Workload              PilotWorkload         `yaml:"workload" json:"workload"`
	Budget                PilotBudget           `yaml:"budget" json:"budget"`
	Environment           PilotEnvironment      `yaml:"environment" json:"environment"`
	AuthorizationAliases  []string              `yaml:"authorization_aliases" json:"authorization_aliases"`
	ScopePredicates       []PilotScopePredicate `yaml:"scope_predicates" json:"scope_predicates"`
	CorrelationPredicates []string              `yaml:"correlation_predicates" json:"correlation_predicates"`
	RequiredSchema        []string              `yaml:"required_schema" json:"required_schema"`
	RequireOrder          bool                  `yaml:"require_order" json:"require_order"`
	RequireLimit          bool                  `yaml:"require_limit" json:"require_limit"`
	RequiredCases         []PilotCase           `yaml:"required_cases" json:"required_cases"`
}

// PilotScopePredicate binds one authorization expression to the named source
// alias. It must be a complete predicate, not a word that may appear in a
// comment, projection, or unrelated branch.
type PilotScopePredicate struct {
	Alias      string `yaml:"alias" json:"alias"`
	Expression string `yaml:"expression" json:"expression"`
}

// PilotWorkload records representative corpus shape and explicit budgets.
type PilotWorkload struct {
	Cardinality string `yaml:"cardinality" json:"cardinality"`
	Skew        string `yaml:"skew" json:"skew"`
	Depth       string `yaml:"depth" json:"depth"`
	Fanout      string `yaml:"fanout" json:"fanout"`
	Payload     string `yaml:"payload" json:"payload"`
	QueryCount  string `yaml:"query_count" json:"query_count"`
	Budgets     string `yaml:"budgets" json:"budgets"`
}

// PilotBudget holds explicit per-case normal-run ceilings. NoiseTolerance
// states the calibration basis; it does not silently relax numeric ceilings.
type PilotBudget struct {
	MaxNormalMilliseconds float64            `yaml:"max_normal_ms" json:"max_normal_ms"`
	MaxQueryCount         int                `yaml:"max_query_count" json:"max_query_count"`
	MaxWork               map[string]float64 `yaml:"max_work" json:"max_work"`
	NoiseTolerance        string             `yaml:"noise_tolerance" json:"noise_tolerance"`
}

// PilotEnvironment names the exact execution and independent oracle inputs.
type PilotEnvironment struct {
	Engine  string `yaml:"engine" json:"engine"`
	Version string `yaml:"version" json:"version"`
	Schema  string `yaml:"schema" json:"schema"`
	Indexes string `yaml:"indexes" json:"indexes"`
	Fixture string `yaml:"fixture" json:"fixture"`
	Oracle  string `yaml:"oracle" json:"oracle"`
	Runner  string `yaml:"runner" json:"runner"`
}

// PilotCase is one required emitted statement variant and safe parameter
// case. The exact emitted query hash is pinned independently of measurements.
type PilotCase struct {
	VariantID   string `yaml:"variant_id" json:"variant_id"`
	CaseID      string `yaml:"case_id" json:"case_id"`
	ScopeMode   string `yaml:"scope_mode" json:"scope_mode"`
	QuerySHA256 string `yaml:"query_sha256" json:"query_sha256"`
}

// PilotCoverageRow explains whether an existing registered entry has an
// executable pilot contract or remains outside this pilot.
type PilotCoverageRow struct {
	EntryID string `json:"entry_id"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
}

// PilotCoverage returns the explicit covered and legacy matrix for one
// manifest. A required pilot ID remains visible even if its entry is missing.
func PilotCoverage(manifest Manifest) []PilotCoverageRow {
	required := make(map[string]struct{}, len(manifest.PilotRequiredIDs))
	for _, id := range manifest.PilotRequiredIDs {
		required[id] = struct{}{}
	}
	rows := make([]PilotCoverageRow, 0, len(manifest.Entries)+len(required))
	seen := make(map[string]struct{}, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		seen[entry.ID] = struct{}{}
		row := PilotCoverageRow{EntryID: entry.ID, Status: "legacy", Reason: "outside executable pilot"}
		if _, ok := required[entry.ID]; ok {
			row.Status, row.Reason = "covered", ""
			if entry.Contract == nil {
				row.Status, row.Reason = "missing", "required contract absent"
			}
		}
		rows = append(rows, row)
	}
	for id := range required {
		if _, ok := seen[id]; !ok {
			rows = append(rows, PilotCoverageRow{EntryID: id, Status: "missing", Reason: "required entry absent"})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].EntryID < rows[j].EntryID })
	return rows
}

// ValidatePilotContracts fails closed for independently required pilot IDs.
// Existing entries without a required pilot ID retain their legacy contract.
func ValidatePilotContracts(manifest Manifest) error {
	entries := make(map[string]Entry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		entries[entry.ID] = entry
	}
	var violations []string
	seenRequired := make(map[string]struct{}, len(manifest.PilotRequiredIDs))
	for _, id := range manifest.PilotRequiredIDs {
		if _, duplicate := seenRequired[id]; duplicate {
			violations = append(violations, fmt.Sprintf("duplicate required pilot %s", id))
		}
		seenRequired[id] = struct{}{}
		entry, ok := entries[id]
		if !ok || entry.Contract == nil {
			violations = append(violations, fmt.Sprintf("pilot %s requires an executable contract", id))
			continue
		}
		violations = append(violations, validatePilotContract(entry)...)
	}
	for _, entry := range manifest.Entries {
		if entry.Contract != nil {
			if _, ok := seenRequired[entry.ID]; !ok {
				violations = append(violations, fmt.Sprintf("%s: contract is not in pilot_required_ids", entry.ID))
			}
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		return errors.New(strings.Join(violations, "; "))
	}
	return nil
}

func validatePilotContract(entry Entry) []string {
	c := entry.Contract
	var violations []string
	if c.Version != 1 {
		violations = append(violations, fmt.Sprintf("%s: unsupported pilot contract version %d", entry.ID, c.Version))
	}
	for label, value := range map[string]string{
		"result": c.Result, "authorization": c.Authorization,
		"current_history": c.CurrentHistory, "nulls": c.Nulls,
		"duplicates": c.Duplicates, "ordering": c.Ordering,
		"pagination": c.Pagination, "partial": c.Partial,
		"rationale": c.Rationale, "index_cost": c.IndexCost,
		"cardinality": c.Workload.Cardinality, "skew": c.Workload.Skew,
		"depth": c.Workload.Depth, "fanout": c.Workload.Fanout,
		"payload": c.Workload.Payload, "query_count": c.Workload.QueryCount,
		"budgets": c.Workload.Budgets,
		"engine":  c.Environment.Engine, "engine_version": c.Environment.Version,
		"schema": c.Environment.Schema, "indexes": c.Environment.Indexes,
		"fixture": c.Environment.Fixture, "oracle": c.Environment.Oracle,
		"runner": c.Environment.Runner,
	} {
		if strings.TrimSpace(value) == "" {
			violations = append(violations, fmt.Sprintf("%s: missing pilot %s", entry.ID, label))
		}
	}
	if len(c.Patterns) == 0 || len(c.Alternatives) == 0 {
		violations = append(violations, fmt.Sprintf("%s: patterns and alternatives are required", entry.ID))
	}
	if c.Budget.MaxNormalMilliseconds <= 0 || math.IsNaN(c.Budget.MaxNormalMilliseconds) || math.IsInf(c.Budget.MaxNormalMilliseconds, 0) || c.Budget.MaxQueryCount <= 0 ||
		len(c.Budget.MaxWork) == 0 || strings.TrimSpace(c.Budget.NoiseTolerance) == "" {
		violations = append(violations, fmt.Sprintf("%s: executable timing, query-count, work, and noise budgets are required", entry.ID))
	}
	for key, ceiling := range c.Budget.MaxWork {
		if strings.TrimSpace(key) == "" || ceiling < 0 || math.IsNaN(ceiling) || math.IsInf(ceiling, 0) {
			violations = append(violations, fmt.Sprintf("%s: invalid work budget %s", entry.ID, key))
		}
	}
	if len(c.RequiredCases) == 0 {
		violations = append(violations, fmt.Sprintf("%s: required_cases is empty", entry.ID))
	}
	seen := make(map[string]struct{}, len(c.RequiredCases))
	for _, candidate := range c.RequiredCases {
		key := candidate.VariantID + "/" + candidate.CaseID
		if strings.TrimSpace(candidate.VariantID) == "" || strings.TrimSpace(candidate.CaseID) == "" || !isSHA256(candidate.QuerySHA256) ||
			(candidate.ScopeMode != "scoped" && candidate.ScopeMode != "all_scopes") {
			violations = append(violations, fmt.Sprintf("%s: malformed required case %s", entry.ID, key))
		}
		if candidate.ScopeMode == "scoped" && len(c.AuthorizationAliases) == 0 && len(c.ScopePredicates) == 0 {
			violations = append(violations, fmt.Sprintf("%s: scoped case %s lacks authorization predicates", entry.ID, key))
		}
		if candidate.ScopeMode == "scoped" && entry.QueryKind == queryKindSQLReadModel && len(c.CorrelationPredicates) == 0 {
			violations = append(violations, fmt.Sprintf("%s: scoped SQL case %s lacks correlation predicates", entry.ID, key))
		}
		if _, duplicate := seen[key]; duplicate {
			violations = append(violations, fmt.Sprintf("%s: duplicate required case %s", entry.ID, key))
		}
		seen[key] = struct{}{}
	}
	if !isSHA256(entry.Source.SourceSHA256) {
		violations = append(violations, fmt.Sprintf("%s: pilot requires source SHA-256", entry.ID))
	}
	return violations
}

func isSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

// PilotContractSHA256 returns the stable JSON digest bound into evidence.
func PilotContractSHA256(contract PilotContract) string {
	data, _ := json.Marshal(contract)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
