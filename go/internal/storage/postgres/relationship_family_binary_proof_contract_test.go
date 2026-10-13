// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/hex"
	"strings"
	"sync"
	"testing"
)

func TestParseRelationshipFamilyBinaryProofPositiveInt(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		raw     string
		want    int64
		wantErr bool
	}{
		{name: "positive", raw: "896", want: 896},
		{name: "trimmed", raw: " 8 ", want: 8},
		{name: "missing", raw: "", wantErr: true},
		{name: "zero", raw: "0", wantErr: true},
		{name: "negative", raw: "-1", wantErr: true},
		{name: "invalid", raw: "eight", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseRelationshipFamilyBinaryProofPositiveInt(tc.raw)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parse error = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("parsed value = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestValidateRelationshipFamilyBinaryProofDatabase(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		database string
		schema   string
		mode     string
		confirm  string
		wantErr  bool
	}{
		{
			name:     "candidate proof",
			database: "ifa_relationship_family_retained_proof",
			schema:   "public",
			mode:     "candidate",
			confirm:  "isolated-write-proof",
		},
		{
			name:     "baseline proof",
			database: "ifa_relationship_family_retained_baseline",
			schema:   "public",
			mode:     "baseline",
			confirm:  "isolated-baseline-proof",
		},
		{name: "retained production database", database: "eshu", schema: "public", mode: "candidate", confirm: "isolated-write-proof", wantErr: true},
		{name: "wrong schema", database: "ifa_relationship_family_retained_proof", schema: "proof", mode: "candidate", confirm: "isolated-write-proof", wantErr: true},
		{name: "missing confirmation", database: "ifa_relationship_family_retained_proof", schema: "public", mode: "candidate", wantErr: true},
		{name: "unknown mode", database: "ifa_relationship_family_retained_proof", schema: "public", mode: "compare", confirm: "isolated-write-proof", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateRelationshipFamilyBinaryProofDatabase(tc.database, tc.schema, tc.mode, tc.confirm)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validation error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestRelationshipFamilyBinaryProofFactIDsTrackDuplicates(t *testing.T) {
	t.Parallel()

	var factIDs relationshipFamilyBinaryProofFactIDs
	factIDs.add("fact-a")
	factIDs.add("fact-b")
	factIDs.add("fact-a")

	got, duplicates := factIDs.snapshot()
	if len(got) != 2 || duplicates != 1 {
		t.Fatalf("fact IDs=%v duplicates=%d, want two unique IDs and one duplicate", got, duplicates)
	}
}

func TestRelationshipFamilyBinaryProofDigestValuesUsesLengthBoundaries(t *testing.T) {
	t.Parallel()

	joined := relationshipFamilyBinaryProofDigestValues([]string{"ab", "c"})
	split := relationshipFamilyBinaryProofDigestValues([]string{"a", "bc"})
	if joined == split {
		t.Fatal("row digest ignored value boundaries")
	}
	if got := len(joined); got != hex.EncodedLen(32) {
		t.Fatalf("digest length = %d, want %d", got, hex.EncodedLen(32))
	}
}

func TestRelationshipFamilyBinaryProofIndexLookupIsSchemaExact(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"JOIN pg_namespace AS namespace",
		"namespace.nspname = current_schema()",
		"table_class.relname = 'fact_records'",
	} {
		if !strings.Contains(relationshipFamilyBinaryProofIndexStateQuery, want) {
			t.Fatalf("index state query missing schema-exact fragment %q", want)
		}
	}
}

func TestRelationshipFamilyBinaryProofOverlapTracker(t *testing.T) {
	t.Parallel()

	tracker := &relationshipFamilyBinaryProofOverlapTracker{}
	const workers = 8
	ready := make(chan struct{}, workers)
	release := make(chan struct{})
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			tracker.begin()
			defer tracker.end()
			ready <- struct{}{}
			<-release
		}()
	}
	for range workers {
		<-ready
	}
	close(release)
	group.Wait()

	if got := tracker.peak(); got != workers {
		t.Fatalf("peak overlap = %d, want %d", got, workers)
	}
	if got := tracker.active(); got != 0 {
		t.Fatalf("active overlap = %d after completion, want 0", got)
	}
}
