// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	payloadstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestProtectedAdvisoryAuthorityRecordsSanitizedAcceptedBatch(t *testing.T) {
	t.Parallel()
	tx := &fakeTx{queryResponses: []queueFakeRows{{rows: [][]any{{1}}}}}
	authority := ProtectedPublicAdvisoryAuthority{
		roleRevision: 5, databaseIdentity: "00c9c993-20fb-4f11-9acd-44202a90948a",
	}
	batch := []facts.Envelope{{
		FactID: "fact-1", FactKind: "vulnerability.cve",
		Payload: map[string]any{"text": "a\x00b"},
	}}
	if err := authority.RecordAcceptedPublicAdvisoryFacts(context.Background(), tx, "receipt-1", batch); err != nil {
		t.Fatal(err)
	}
	if len(tx.queries) != 1 || tx.queries[0].query != publicAdvisoryRecordFactsQuery ||
		len(tx.queries[0].args) != 4 || tx.queries[0].args[0] != "receipt-1" ||
		tx.queries[0].args[2] != int64(5) {
		t.Fatalf("protected batch query = %+v", tx.queries)
	}
	var sent []struct {
		FactID   string          `json:"fact_id"`
		FactKind string          `json:"fact_kind"`
		Payload  json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal([]byte(tx.queries[0].args[1].(string)), &sent); err != nil {
		t.Fatal(err)
	}
	wantPayload, err := payloadstore.MarshalPayload(batch[0].Payload)
	if err != nil || len(sent) != 1 || sent[0].FactID != batch[0].FactID ||
		sent[0].FactKind != batch[0].FactKind || string(sent[0].Payload) != string(wantPayload) {
		t.Fatalf("recorded batch = %+v, want sanitizer payload %q", sent, wantPayload)
	}
	if strings.Contains(string(sent[0].Payload), `\u0000`) {
		t.Fatal("unsanitized JSONB null escape reached protected batch")
	}
}

func TestProtectedAdvisoryAuthorityPublishesInSeparateTransaction(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, value string
		published   bool
	}{
		{name: "complete receipt", published: true},
		{name: "missing receipt", published: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &fakeTx{queryResponses: []queueFakeRows{{rows: [][]any{{tc.published}}}}}
			database := &fakeTransactionalDB{tx: tx}
			authority, err := NewProtectedPublicAdvisoryAuthority(database, 5,
				"00c9c993-20fb-4f11-9acd-44202a90948a")
			if err != nil {
				t.Fatal(err)
			}
			published, err := authority.PublishCompletePublicAdvisoryGeneration(context.Background(),
				workflow.ClaimMutation{WorkItemID: "work", ClaimID: "claim", FencingToken: 3, OwnerID: "owner"},
				"vuln-intel://kev", "generation")
			if err != nil || published != tc.published || tx.committed != tc.published ||
				tx.rolledBack == tc.published || len(tx.queries) != 1 ||
				tx.queries[0].query != publicAdvisoryPublishQuery {
				t.Fatalf("publication = %t, %v; transaction=%+v", published, err, tx)
			}
		})
	}
}
