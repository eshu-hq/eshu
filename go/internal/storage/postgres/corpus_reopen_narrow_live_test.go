// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// Per-item read-set waits (#10877): narrowed reopen live proofs.
//
// On the full production bootstrap schema, on a disposable database:
// workload_materialization and service_catalog_correlation record the
// narrowed read's input fingerprint on success; the changed-evidence gate
// leaves them succeeded while their input is unchanged and reopens them when
// a late-arriving foreign scope changes it; a stale claim cannot overwrite a
// newer fingerprint. Gated on ESHU_REDUCER_FAIRNESS_PROOF_DSN or
// ESHU_POSTGRES_DSN.

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10877 narrowed reopen: fingerprint record and changed-evidence gate for workload and service catalog on the production schema
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

// recordNarrowGateInput records one domain's narrowed fingerprint through
// the production claim fence.
func recordNarrowGateInput(
	t *testing.T, ctx context.Context,
	store *RelationshipStore, claim fingerprintGateClaim,
	fingerprint reducercontract.ResolvedReadFingerprint,
	domain reducercontract.Domain,
) {
	t.Helper()
	var err error
	switch domain {
	case reducercontract.DomainWorkloadMaterialization:
		err = store.RecordWorkloadMaterializationReopenFingerprint(ctx, claim.workItemID, claim.claimedAt, fingerprint)
	case reducercontract.DomainServiceCatalogCorrelation:
		err = store.RecordServiceCatalogCorrelationReopenFingerprint(ctx, claim.workItemID, claim.claimedAt, fingerprint)
	default:
		t.Fatalf("record narrow gate input: unexpected domain %q", domain)
	}
	if err != nil {
		t.Fatalf("record %s fingerprint for %s: %v", domain, claim.workItemID, err)
	}
}

func TestNarrowedReopenFingerprintGateLive(t *testing.T) {
	ctx := context.Background()
	db, _ := openNarrowFenceDB(t, ctx)
	store := NewRelationshipStore(SQLDB{DB: db})
	at := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)

	// Consumer scope A (repo-a) active; producer scope B (repo-b -> repo-a)
	// active too, so the narrowed fence is complete.
	seedFingerprintGateScope(t, ctx, db, fingerprintGateScope{scopeID: "git:10877-a", genID: "gen-10877-a", repoID: "repo-a"})
	seedFingerprintGateScope(t, ctx, db, fingerprintGateScope{scopeID: "git:10877-b", genID: "gen-10877-b", repoID: "repo-b"})
	upsertFingerprintGateDeploysFrom(t, ctx, db, "gen-10877-b", "repo-b", "repo-a")

	domains := []reducercontract.Domain{
		reducercontract.DomainWorkloadMaterialization,
		reducercontract.DomainServiceCatalogCorrelation,
	}
	workItemIDs := map[reducercontract.Domain]string{
		reducercontract.DomainWorkloadMaterialization:   "wl-10877-a",
		reducercontract.DomainServiceCatalogCorrelation: "sc-10877-a",
	}
	for _, domain := range domains {
		claim := claimFingerprintGateItem(t, ctx, db, workItemIDs[domain], "git:10877-a", "gen-10877-a", domain, at)
		_, _, _, fingerprint, err := store.GetFingerprintedResolvedRelationshipsForReposWithRestrictedFence(ctx, []string{"repo-a"})
		if err != nil {
			t.Fatalf("narrowed fingerprinted read for %s: %v", domain, err)
		}
		if fingerprint.Value == "" || fingerprint.Value == reducercontract.EmptyResolvedReadFingerprint {
			t.Fatalf("narrowed fingerprint for %s = %q, want the input digest", domain, fingerprint.Value)
		}
		recordNarrowGateInput(t, ctx, store, claim, fingerprint, domain)
		ackFingerprintGateItem(t, ctx, db, workItemIDs[domain], at)
	}

	// Unchanged input stays succeeded on both domains.
	for _, domain := range domains {
		reopen, skipped, err := listCorpusReopenCandidates(ctx, SQLDB{DB: db},
			backfill.ListCorpusReopenCandidatesQueries[string(domain)], string(domain))
		if err != nil {
			t.Fatalf("list %s reopen candidates: %v", domain, err)
		}
		if len(reopen) != 0 || skipped != 1 {
			t.Fatalf("%s gate after no change = reopen %v skipped %d, want [] and 1", domain, reopen, skipped)
		}
	}

	// A stale claim cannot overwrite the recorded fingerprint: recording
	// with an older claim after the success write must fail closed.
	stale := fingerprintGateClaim{workItemID: "wl-10877-a", claimedAt: at.Add(-time.Hour)}
	if err := store.RecordWorkloadMaterializationReopenFingerprint(ctx, stale.workItemID, stale.claimedAt,
		reducercontract.ResolvedReadFingerprint{RepoIDs: []string{"repo-a"}, Value: "stale"}); err == nil {
		t.Fatal("stale workload fingerprint record = nil, want ErrExecutionClaimRejected")
	} else if !errors.Is(err, reducercontract.ErrExecutionClaimRejected) {
		t.Fatalf("stale workload fingerprint record error = %v, want ErrExecutionClaimRejected", err)
	}

	// Late arrival: a new resolved row touching the read set lands under the
	// already-active producer generation. Both succeeded items reopen.
	upsertFingerprintGateDeploysFrom(t, ctx, db, "gen-10877-b", "repo-b2", "repo-a")
	for _, domain := range domains {
		reopen, skipped, err := listCorpusReopenCandidates(ctx, SQLDB{DB: db},
			backfill.ListCorpusReopenCandidatesQueries[string(domain)], string(domain))
		if err != nil {
			t.Fatalf("list %s reopen candidates after late arrival: %v", domain, err)
		}
		if len(reopen) != 1 || reopen[0] != workItemIDs[domain] {
			t.Fatalf("%s gate after late arrival = reopen %v, want [%s]", domain, reopen, workItemIDs[domain])
		}
		if skipped != 0 {
			t.Fatalf("%s gate after late arrival skipped = %d, want 0", domain, skipped)
		}
	}
}
