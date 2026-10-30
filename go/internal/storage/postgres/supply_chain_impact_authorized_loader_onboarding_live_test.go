// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9299 disposable PostgreSQL proof that the authorized impact loader leaves the write on the plain static path when role policy was never installed, instead of admitting a protected set the writer cannot authorize
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_POSTGRES_DISPOSABLE

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// staticOnlyExpansion returns the canned envelopes as a complete static
// load without touching the database, so the proof isolates the loader's
// protection-engagement decision from evidence availability.
func staticOnlyExpansion(envelopes []facts.Envelope) supplychaincore.AuthorizedStaticSupplyChainImpactExpansion {
	return func(context.Context, factload.FactLoader, []facts.Envelope) (supplychaincore.AuthorizedStaticSupplyChainImpactLoad, error) {
		return supplychaincore.AuthorizedStaticSupplyChainImpactLoad{
			Envelopes:            envelopes,
			SuppressionEnvelopes: envelopes,
			Complete:             true,
			CoverageReason:       "",
		}, nil
	}
}

func onboardingProofIntent() reducercontract.Intent {
	return reducercontract.Intent{
		ScopeID:      "onboarding-proof-scope",
		GenerationID: "onboarding-proof-generation-1",
		Domain:       reducercontract.DomainSupplyChainImpact,
	}
}

func checkPlainStaticLoad(t *testing.T, loaded supplychaincore.AuthorizedSupplyChainImpactLoad) {
	t.Helper()
	if !loaded.Complete {
		t.Fatalf("loader Complete = false, want true: a store without installed policy must stay on the plain static path, not report incomplete coverage")
	}
	if loaded.ProtectionEngaged {
		t.Fatalf("loader ProtectionEngaged = true with no installed role policy: the admitted set would dead-letter in authorize")
	}
	if len(loaded.StaticEnvelopes) != 1 || loaded.StaticEnvelopes[0].FactID != "onboarding-static-fact-1" {
		t.Fatalf("loader dropped the static envelopes on the unprotected path: %+v", loaded.StaticEnvelopes)
	}
	if len(loaded.Targets) != 0 || len(loaded.ScanPublications) != 0 {
		t.Fatalf("loader admitted targets with no installed role policy: %+v", loaded.ScanPublications)
	}
}

// TestAuthorizedImpactLoaderWithoutPolicyStateLeavesPlainWriteLive boots a
// full public schema with no role-policy install (the pre-onboarding
// production shape behind the Ifa gate dead-letters) and requires the
// loader to report the static load complete but protection disengaged.
func TestAuthorizedImpactLoaderWithoutPolicyStateLeavesPlainWriteLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "onboarding_norow",
		"set ESHU_POSTGRES_DSN and ESHU_POSTGRES_DISPOSABLE=1 to run the loader onboarding proof")
	loader := NewAuthorizedSupplyChainImpactEvidenceLoader(SQLDB{DB: database})
	if loader == nil {
		t.Fatal("authorized impact loader is unavailable on a repeatable-read store")
	}
	envelopes := []facts.Envelope{{FactID: "onboarding-static-fact-1", ScopeID: "onboarding-proof-scope", GenerationID: "onboarding-proof-generation-1"}}
	loaded, err := loader.ListAuthorizedSupplyChainImpactEvidence(ctx, onboardingProofIntent(), staticOnlyExpansion(envelopes))
	if err != nil {
		t.Fatalf("loader without installed policy: %v", err)
	}
	checkPlainStaticLoad(t, loaded)
}

// TestAuthorizedImpactLoaderWithoutAuthorityTablesLeavesPlainWriteLive
// repeats the proof on an isolated schema where the authority layout is
// skipped entirely, so the engagement probe meets a missing relation
// instead of an empty one.
func TestAuthorizedImpactLoaderWithoutAuthorityTablesLeavesPlainWriteLive(t *testing.T) {
	adminDSN := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if adminDSN == "" {
		t.Skip("set ESHU_POSTGRES_DSN and ESHU_POSTGRES_DISPOSABLE=1 to run the loader onboarding proof")
	}
	if os.Getenv("ESHU_POSTGRES_DISPOSABLE") != "1" {
		t.Skip("set ESHU_POSTGRES_DISPOSABLE=1 to run this proof on a disposable PostgreSQL database (absent opt-in skips, never fails)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	schema := fmt.Sprintf("onboarding_noschema_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	})
	sep := "?"
	if strings.Contains(adminDSN, "?") {
		sep = "&"
	}
	isolated, err := sql.Open("pgx", adminDSN+sep+"search_path="+schema)
	if err != nil {
		t.Fatalf("open isolated pool: %v", err)
	}
	t.Cleanup(func() { _ = isolated.Close() })
	if err := ApplyBootstrap(ctx, SQLDB{DB: isolated}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	loader := NewAuthorizedSupplyChainImpactEvidenceLoader(SQLDB{DB: isolated})
	if loader == nil {
		t.Fatal("authorized impact loader is unavailable on a repeatable-read store")
	}
	envelopes := []facts.Envelope{{FactID: "onboarding-static-fact-1", ScopeID: "onboarding-proof-scope", GenerationID: "onboarding-proof-generation-1"}}
	loaded, err := loader.ListAuthorizedSupplyChainImpactEvidence(ctx, onboardingProofIntent(), staticOnlyExpansion(envelopes))
	if err != nil {
		t.Fatalf("loader without authority tables: %v", err)
	}
	checkPlainStaticLoad(t, loaded)
}
