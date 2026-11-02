// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// disengagedPolicyDB answers the protection-engagement probe with no rows, the
// state of a store that never installed role policy, and rejects every other
// statement: the static expansion under test is a callback that reads nothing.
type disengagedPolicyDB struct{}

func (disengagedPolicyDB) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	if query == authorizedImpactProtectionEngagedQuery {
		return &queueFakeRows{}, nil
	}
	return nil, sql.ErrConnDone
}

func (disengagedPolicyDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, sql.ErrConnDone
}

func (disengagedPolicyDB) BeginReadOnlyRepeatableRead(context.Context) (db.Transaction, error) {
	return nil, sql.ErrConnDone
}

// TestAuthorizedLoaderListsFoldedFilesAsStaticEvidence is the #9855 F1
// regression. A folded pass hands the loader one synthetic digest envelope in
// place of its JS/TS and Python file facts. The protected writer accepts a
// finding's evidence fact ID only when the loader listed it, so the loader must
// list the folded file IDs, not the digest's synthetic ID.
func TestAuthorizedLoaderListsFoldedFilesAsStaticEvidence(t *testing.T) {
	t.Parallel()

	fold := supplychaincore.NewFileReachabilityFold(nil)
	for _, id := range []string{"file-a", "file-b"} {
		fold.Add(facts.Envelope{FactID: id, FactKind: factload.FactKindFile, Payload: map[string]any{"repo_id": "repo"}})
	}
	digest, ok := fold.DigestEnvelope()
	if !ok {
		t.Fatal("DigestEnvelope() ok = false, want a digest for two folded files")
	}
	scope := facts.Envelope{FactID: "scope-fact", FactKind: "package_registry.package"}

	loader := NewAuthorizedSupplyChainImpactEvidenceLoader(disengagedPolicyDB{})
	loaded, err := loader.ListAuthorizedSupplyChainImpactEvidence(
		context.Background(),
		reducercontract.Intent{ScopeID: "scope", GenerationID: "gen", Domain: reducercontract.DomainSupplyChainImpact},
		func(context.Context, factload.FactLoader, []facts.Envelope) (supplychaincore.AuthorizedStaticSupplyChainImpactLoad, error) {
			return supplychaincore.AuthorizedStaticSupplyChainImpactLoad{
				Envelopes: []facts.Envelope{scope, digest}, Complete: true,
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("ListAuthorizedSupplyChainImpactEvidence() error = %v", err)
	}
	want := []string{"file-a", "file-b", "scope-fact"}
	got := slices.Clone(loaded.StaticNonAdvisoryEvidenceFactIDs)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("StaticNonAdvisoryEvidenceFactIDs = %v, want %v", got, want)
	}
}
