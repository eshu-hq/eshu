// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
)

func TestReaderWitnessPoolOutputIsBoundedAndRedacted(t *testing.T) {
	terms := []string{"secret-alpha", "secret-beta"}
	baseline := []codetopicparallel.ProbeRow{
		probe("entity", terms[0], "private-entity-one"),
		probe("entity", terms[0], "private-entity-two"),
		probe("file", terms[1], "private-file"),
	}
	candidate := []codetopicparallel.ProbeRow{
		probe("entity", terms[0], "private-entity-three"),
		probe("entity", terms[0], "private-entity-four"),
		probe("file", terms[1], "private-file"),
	}
	var output strings.Builder
	if err := writeReaderWitnessPools(&output, baseline, candidate, terms, 2, true); err != nil {
		t.Fatal(err)
	}
	want := "reader_witness_pool term_index=0 baseline_entity_rows=2 baseline_file_rows=0 candidate_entity_rows=2 candidate_file_rows=0 baseline_entity_capped=true baseline_file_capped=false candidate_entity_capped=true candidate_file_capped=false baseline_eligibility=pass candidate_eligibility=pass\n" +
		"reader_witness_pool term_index=1 baseline_entity_rows=0 baseline_file_rows=1 candidate_entity_rows=0 candidate_file_rows=1 baseline_entity_capped=false baseline_file_capped=false candidate_entity_capped=false candidate_file_capped=false baseline_eligibility=pass candidate_eligibility=pass\n"
	if got := output.String(); got != want {
		t.Fatalf("pool output:\n%s\nwant:\n%s", got, want)
	}
	for _, private := range []string{"secret-alpha", "secret-beta", "private-entity", "private-file"} {
		if strings.Contains(output.String(), private) {
			t.Fatalf("pool output leaked %q", private)
		}
	}
}

func TestReaderWitnessPoolOutputRejectsInvalidRowsBeforePrinting(t *testing.T) {
	var output strings.Builder
	rows := []codetopicparallel.ProbeRow{probe("entity", "unexpected", "private")}
	if err := writeReaderWitnessPools(&output, rows, nil, []string{"known"}, 2, true); err == nil {
		t.Fatal("unexpected term accepted")
	}
	if output.Len() != 0 {
		t.Fatalf("partial output after invalid pool: %q", output.String())
	}
	if err := writeReaderWitnessPools(&output, nil, nil, []string{"known"}, 2, false); err == nil {
		t.Fatal("unverified eligibility accepted")
	}
}
