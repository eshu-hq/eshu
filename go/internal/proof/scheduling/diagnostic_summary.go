// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
)

type diagnosticPool struct {
	count        int
	capped       bool
	multisetHash string
	arrivalHash  string
}

func summarizeDiagnosticPools(rows []codetopicparallel.ProbeRow, terms []string, cap int) (map[poolKey]diagnosticPool, error) {
	if cap <= 0 {
		return nil, fmt.Errorf("diagnostic pool cap must be positive")
	}
	allowed := make(map[string]struct{}, len(terms))
	ordered := make(map[poolKey][]string, 2*len(terms))
	for _, term := range terms {
		if term == "" {
			return nil, fmt.Errorf("diagnostic term is empty")
		}
		if _, duplicate := allowed[term]; duplicate {
			return nil, fmt.Errorf("diagnostic term is duplicated")
		}
		allowed[term] = struct{}{}
		ordered[poolKey{kind: "entity", term: term}] = nil
		ordered[poolKey{kind: "file", term: term}] = nil
	}
	for _, row := range rows {
		if _, ok := allowed[row.MatchedTerm]; !ok {
			return nil, fmt.Errorf("diagnostic probe returned an unexpected term")
		}
		if row.SourceKind != "entity" && row.SourceKind != "file" {
			return nil, fmt.Errorf("diagnostic probe returned an unexpected source kind")
		}
		encoded, err := json.Marshal(row)
		if err != nil {
			return nil, fmt.Errorf("encode diagnostic probe row: %w", err)
		}
		key := poolKey{kind: row.SourceKind, term: row.MatchedTerm}
		ordered[key] = append(ordered[key], string(encoded))
		if len(ordered[key]) > cap {
			return nil, fmt.Errorf("diagnostic probe pool exceeds cap")
		}
	}
	summary := make(map[poolKey]diagnosticPool, len(ordered))
	for key, arrival := range ordered {
		sorted := slices.Clone(arrival)
		slices.Sort(sorted)
		summary[key] = diagnosticPool{
			count:        len(arrival),
			capped:       len(arrival) == cap,
			multisetHash: hashStrings(sorted),
			arrivalHash:  hashStrings(arrival),
		}
	}
	return summary, nil
}

// assembledDiagnosticRow mirrors the columns of the unchanged AssemblySQL.
// Nullable sort keys remain pointers so a NULL differs from an empty string.
type assembledDiagnosticRow struct {
	SourceKind    string  `json:"source_kind"`
	RepoID        *string `json:"repo_id"`
	RelativePath  *string `json:"relative_path"`
	EntityID      *string `json:"entity_id"`
	EntityName    *string `json:"entity_name"`
	EntityType    *string `json:"entity_type"`
	Language      *string `json:"language"`
	StartLine     *int32  `json:"start_line"`
	EndLine       *int32  `json:"end_line"`
	MatchedTerms  *string `json:"matched_terms"`
	Score         int32   `json:"score"`
	PoolTruncated bool    `json:"pool_truncated"`
}

type diagnosticPage struct {
	rows             []assembledDiagnosticRow
	fingerprints     []string
	fullHash         string
	visibleHash      string
	lookaheadHash    string
	lookaheadPresent bool
}

func summarizeDiagnosticPage(rows []assembledDiagnosticRow) (diagnosticPage, error) {
	if len(rows) > 26 {
		return diagnosticPage{}, fmt.Errorf("diagnostic assembly exceeded 26-row page bound")
	}
	fingerprints := make([]string, 0, len(rows))
	for _, row := range rows {
		encoded, err := json.Marshal(row)
		if err != nil {
			return diagnosticPage{}, fmt.Errorf("encode assembled diagnostic row: %w", err)
		}
		fingerprints = append(fingerprints, string(encoded))
	}
	visibleCount := min(len(rows), 25)
	page := diagnosticPage{
		rows:         rows,
		fingerprints: fingerprints,
		fullHash:     hashStrings(fingerprints),
		visibleHash:  hashStrings(fingerprints[:visibleCount]),
	}
	if len(rows) == 26 {
		page.lookaheadPresent = true
		page.lookaheadHash = hashStrings(fingerprints[25:])
	}
	return page, nil
}

func diagnosticTextRank(label string, value *string) string {
	if value == nil {
		return label + "_null=true " + label + "_sha256=none"
	}
	sum := sha256.Sum256([]byte(*value))
	return label + "_null=false " + label + "_sha256=" + hex.EncodeToString(sum[:])
}

func diagnosticRank(row *assembledDiagnosticRow) string {
	if row == nil {
		return "missing=true"
	}
	return fmt.Sprintf("missing=false score=%d %s %s %s source_kind=%s",
		row.Score,
		diagnosticTextRank("repo_id", row.RepoID),
		diagnosticTextRank("relative_path", row.RelativePath),
		diagnosticTextRank("entity_name", row.EntityName),
		row.SourceKind)
}

func formatDiagnosticFirstDifference(leftName, rightName string, left, right diagnosticPage, collation, provider, locale string) string {
	length := max(len(left.fingerprints), len(right.fingerprints))
	index := -1
	for i := range length {
		if i >= len(left.fingerprints) || i >= len(right.fingerprints) || left.fingerprints[i] != right.fingerprints[i] {
			index = i
			break
		}
	}
	if index == -1 {
		return fmt.Sprintf("diagnostic_first_difference left=%s right=%s first_difference=none", leftName, rightName)
	}
	region := "visible"
	if index == 25 {
		region = "lookahead"
	}
	var leftRow, rightRow *assembledDiagnosticRow
	if index < len(left.rows) {
		leftRow = &left.rows[index]
	}
	if index < len(right.rows) {
		rightRow = &right.rows[index]
	}
	return strings.Join([]string{
		fmt.Sprintf("diagnostic_first_difference left=%s right=%s rank_index=%d region=%s", leftName, rightName, index, region),
		fmt.Sprintf("collation=%s provider=%s locale=%s", collation, provider, locale),
		"left_rank={" + diagnosticRank(leftRow) + "}",
		"right_rank={" + diagnosticRank(rightRow) + "}",
	}, " ")
}
