// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestListActiveSupplyChainImpactFactsPagesIdentityAfterLegacyCompletes(t *testing.T) {
	baseTime := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)
	firstIdentityPage := make([][]any, listFactsByKindPageSize)
	for index := range firstIdentityPage {
		firstIdentityPage[index] = taggedSupplyChainImpactRow(
			1,
			int64(index+1),
			containerImageIdentitySupportFactRow("repository:paging", "sha256:paging", index+1, baseTime),
		)
	}
	core := taggedSupplyChainImpactRow(0, 1, supplyChainImpactFactRow(
		"vulnerability:cve:paging",
		"vulnerability.cve",
		baseTime,
	))
	firstPage := append([][]any{core}, firstIdentityPage...)
	lastIdentity := taggedSupplyChainImpactRow(
		1,
		1,
		containerImageIdentitySupportFactRow("repository:paging", "sha256:paging", listFactsByKindPageSize+1, baseTime),
	)
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: firstPage},
		{rows: [][]any{lastIdentity}},
	}}

	loaded, truncated, err := NewFactStore(db).ListActiveSupplyChainImpactFacts(
		context.Background(),
		reducer.SupplyChainImpactFactFilter{SubjectDigests: []string{"sha256:paging"}},
	)
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts() error = %v, want nil", err)
	}
	if truncated {
		t.Fatal("truncated = true, want false")
	}
	if got, want := len(loaded), listFactsByKindPageSize+2; got != want {
		t.Fatalf("len(loaded) = %d, want %d", got, want)
	}
	if loaded[0].FactID != "vulnerability:cve:paging" {
		t.Fatalf("first result = %q, want core evidence first", loaded[0].FactID)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if got := db.queries[1].args[11]; got != 0 {
		t.Fatalf("second legacy limit = %v, want 0 after legacy completed", got)
	}
	wantIdentityCursor := firstIdentityPage[len(firstIdentityPage)-1][2].(string)
	if got := db.queries[1].args[19]; got != wantIdentityCursor {
		t.Fatalf("second identity cursor = %q, want %q", got, wantIdentityCursor)
	}
}

func TestListActiveSupplyChainImpactFactsCapsSuppressionWhileIdentityContinues(t *testing.T) {
	originalCap := maxSupplyChainImpactActiveEvidenceRowsPerCall
	maxSupplyChainImpactActiveEvidenceRowsPerCall = 1
	t.Cleanup(func() { maxSupplyChainImpactActiveEvidenceRowsPerCall = originalCap })

	baseTime := time.Date(2026, time.July, 31, 11, 0, 0, 0, time.UTC)
	firstPage := make([][]any, 0, listFactsByKindPageSize+2)
	for index := range listFactsByKindPageSize {
		firstPage = append(firstPage, taggedSupplyChainImpactRow(
			1,
			int64(index+1),
			containerImageIdentitySupportFactRow("repository:cap", "sha256:cap", index+1, baseTime),
		))
	}
	firstPage = append(firstPage, suppressionRowCapFakePage(0, 2, baseTime)...)
	lastIdentity := taggedSupplyChainImpactRow(
		1,
		1,
		containerImageIdentitySupportFactRow("repository:cap", "sha256:cap", listFactsByKindPageSize+1, baseTime),
	)
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: firstPage},
		{rows: [][]any{lastIdentity}},
	}}

	loaded, truncated, err := NewFactStore(db).ListActiveSupplyChainImpactFacts(
		context.Background(),
		reducer.SupplyChainImpactFactFilter{SubjectDigests: []string{"sha256:cap"}},
	)
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts() error = %v, want nil", err)
	}
	if !truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := len(loaded), listFactsByKindPageSize+2; got != want {
		t.Fatalf("len(loaded) = %d, want %d identities plus the retained suppression", got, want)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if got := db.queries[1].args[11]; got != 0 {
		t.Fatalf("second legacy limit = %v, want 0 after suppression sentinel", got)
	}
}

func TestListActiveSupplyChainImpactFactsRejectsInvalidTaggedStreams(t *testing.T) {
	baseTime := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	validIdentity := containerImageIdentitySupportFactRow("repository:invalid", "sha256:invalid", 1, baseTime)
	wrongKindIdentity := append([]any(nil), validIdentity...)
	wrongKindIdentity[3] = "vulnerability.cve"
	duplicateID := validIdentity[0].(string)

	tests := []struct {
		name    string
		rows    [][]any
		wantErr string
	}{
		{
			name:    "identity ordinal gap",
			rows:    [][]any{taggedSupplyChainImpactRow(1, 2, validIdentity)},
			wantErr: "stream 1 ordinal",
		},
		{
			name:    "identity wrong kind",
			rows:    [][]any{taggedSupplyChainImpactRow(1, 1, wrongKindIdentity)},
			wantErr: "identity stream fact kind",
		},
		{
			name:    "suppression wrong kind",
			rows:    [][]any{taggedSupplyChainImpactRow(2, 1, supplyChainImpactFactRow("core:wrong-rank", "vulnerability.cve", baseTime))},
			wantErr: "suppression stream fact kind",
		},
		{
			name: "cross stream duplicate",
			rows: [][]any{
				taggedSupplyChainImpactRow(0, 1, supplyChainImpactFactRow(duplicateID, "vulnerability.cve", baseTime)),
				taggedSupplyChainImpactRow(1, 1, validIdentity),
			},
			wantErr: "duplicate fact ID",
		},
		{
			name:    "unknown rank",
			rows:    [][]any{taggedSupplyChainImpactRow(3, 1, supplyChainImpactFactRow("unknown:rank", "vulnerability.cve", baseTime))},
			wantErr: "stream rank 3",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: test.rows}}}
			_, _, err := NewFactStore(db).ListActiveSupplyChainImpactFacts(
				context.Background(),
				reducer.SupplyChainImpactFactFilter{SubjectDigests: []string{"sha256:invalid"}},
			)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func taggedSupplyChainImpactRow(rank int, ordinal int64, envelopeRow []any) []any {
	return append([]any{rank, ordinal}, envelopeRow...)
}

func supplyChainImpactFactRow(factID string, factKind string, observedAt time.Time) []any {
	return []any{
		factID,
		"repository:supply-paging",
		"generation:supply-paging",
		factKind,
		"stable:" + factID,
		"1.0.0",
		"synthetic",
		int64(1),
		"observed",
		"synthetic",
		factID,
		"",
		"",
		observedAt,
		false,
		[]byte(`{}`),
	}
}
