// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"fmt"
	"io"

	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
)

// writeReaderWitnessPools emits bounded, redacted first-route pool counts.
// Term indexes refer to the fixed canonical workload order, never term text.
func writeReaderWitnessPools(output io.Writer, baseline, candidate []codetopicparallel.ProbeRow, terms []string, cap int, persistedVerified bool) error {
	if !persistedVerified {
		return fmt.Errorf("reader witness persisted eligibility has not been verified")
	}
	if len(terms) == 0 || len(terms) > oracleMaxTerms {
		return fmt.Errorf("reader witness requires 1..%d terms", oracleMaxTerms)
	}
	if err := validateConditionalPools(baseline, candidate, terms, cap); err != nil {
		return err
	}
	allowed := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		allowed[term] = struct{}{}
	}
	before, err := indexPools(baseline, allowed, cap)
	if err != nil {
		return err
	}
	after, err := indexPools(candidate, allowed, cap)
	if err != nil {
		return err
	}
	var buffer bytes.Buffer
	for index, term := range terms {
		baselineEntity := len(before[poolKey{kind: "entity", term: term}])
		baselineFile := len(before[poolKey{kind: "file", term: term}])
		candidateEntity := len(after[poolKey{kind: "entity", term: term}])
		candidateFile := len(after[poolKey{kind: "file", term: term}])
		if _, err := fmt.Fprintf(&buffer, "reader_witness_pool term_index=%d baseline_entity_rows=%d baseline_file_rows=%d candidate_entity_rows=%d candidate_file_rows=%d baseline_entity_capped=%t baseline_file_capped=%t candidate_entity_capped=%t candidate_file_capped=%t baseline_eligibility=pass candidate_eligibility=pass\n",
			index, baselineEntity, baselineFile, candidateEntity, candidateFile,
			baselineEntity == cap, baselineFile == cap, candidateEntity == cap, candidateFile == cap); err != nil {
			return fmt.Errorf("format reader witness pool %d: %w", index, err)
		}
	}
	if _, err := output.Write(buffer.Bytes()); err != nil {
		return fmt.Errorf("write reader witness pools: %w", err)
	}
	return nil
}
