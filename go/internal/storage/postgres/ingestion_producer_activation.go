// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Producer-activation dependency index (#7635). When a scope that produces
// evidence for the correlation domains activates a new generation with no
// later ingestion commit, the epoch whole pass never runs and the consumers
// keep the answer they computed against the producer's old generation.
// ProjectorQueue.Ack owes one producer_activation_obligations row for the
// activation; the settle below reopens the succeeded consumer items that
// wait on the owed generation. The epoch whole pass is unchanged.
//
// Per-domain table (arbiter rulings, #7635). Producer evidence is what Ack
// probes; linkage is what the settle reopens; staleness bounds both to items
// that completed before the obligation was owed.
//
//   - container_image_identity. Producer: the activated generation carries
//     identity-filter facts (the loader's verbatim filter). Linkage: the
//     consumer scope's active OCI keys (digests; repo_key+tag pairs)
//     intersect the owed generation's keys. Rationale: identity joins every
//     consumer image against the global active manifest set, so only a key
//     intersection proves the owed generation could change the answer.
//   - kubernetes_correlation_materialization. Same producer and linkage as
//     identity: the materializer resolves live image references through the
//     same shared loader. Pod image_refs, container images and CRI-resolved
//     digests are parsed with the ParseContainerImageRef mirror.
//   - aws_cloud_runtime_drift. Producer: the activated generation carries
//     terraform_state_resource facts with a joinable ARN. Linkage: the drift
//     item's own generation holds aws_resource ARNs intersecting the owed
//     ARNs. Readiness defers drift only before success, so the post-success
//     race still needs this reopen; the staleness conjunct keeps readiness
//     from paying twice (an item that completed after the obligation already
//     read the new state).
//   - deployable_unit_correlation. No producer obligation. DU reads active
//     resolved relationships touching its candidate repos as source OR
//     target, and the #7584 consumer's targeted pass already reopens those
//     domains over the relationship-evidence closure (owed plus evidence
//     sources touching an owed repository), which covers both endpoints'
//     partitions symmetrically. A second reopen would pay the cost twice.
//
// Cost: each settle reads the owed generation's keys once, then probes the
// floored candidates' scopes only (correlated EXISTS against the key
// arrays), never the corpus, so bulk import costs O(keys + dependents),
// not O(Acks x domain). Every listing derives from the shipped correlation
// listing, keeping its replay floor and failed-generation exclusion: the
// settle cannot resurrect the superseded-generation churn #7637 removed.

// producerSettleOwner and producerSettleLease identify the package-level
// settle driver's claims. The resolution-engine runner claims under its own
// owner when wired.
const (
	producerSettleOwner = "producer-activation-settle"
	producerSettleLease = time.Minute
)

// ErrProducerLeaseLost reports that the producer obligation lease expired
// while the settle held its transaction. The transaction rolled back, so the
// reopen did not survive, and the next owner repeats it.
var ErrProducerLeaseLost = fmt.Errorf("producer activation obligation lease expired during settle")

// GenerationCarriesProducerEvidence reports whether the generation carries
// evidence a correlation consumer reads: identity-filter facts or
// terraform_state_resource facts with a joinable ARN. The producer settle
// calls it once per obligation; it is one generation-anchored EXISTS probe
// (TestProducerEvidenceProbeCostLive pins the plan). It must stay out of
// the Ack transaction: it plans in ~8.4 ms per call through the Go driver
// (F1), so Ack owes unconditionally and the settle retires non-producers as
// inapplicable instead.
func GenerationCarriesProducerEvidence(ctx context.Context, queryer db.Queryer, scopeID, generationID string) (bool, error) {
	rows, err := queryer.QueryContext(ctx, producerEvidenceExistsQuery, scopeID, generationID)
	if err != nil {
		return false, fmt.Errorf("probe producer evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("probe producer evidence: %w", err)
		}
		return false, fmt.Errorf("probe producer evidence: no rows returned")
	}
	var carries bool
	if err := rows.Scan(&carries); err != nil {
		return false, fmt.Errorf("probe producer evidence: scan: %w", err)
	}
	return carries, rows.Err()
}

// producerOCIKeySet is the owed generation's OCI linkage keys, split for the
// listing's array parameters: every non-null digest, plus the (repo_key,
// tag) pairs in lockstep for rows carrying both.
type producerOCIKeySet struct {
	digests   []string
	pairRepos []string
	pairTags  []string
}

// listProducerOwedOCIKeys reads the owed generation's OCI linkage keys in
// one probe. It returns empty (non-nil) slices when the generation extracts
// no keys, so the listing matches nothing.
func listProducerOwedOCIKeys(ctx context.Context, queryer db.Queryer, scopeID, generationID string) (producerOCIKeySet, error) {
	rows, err := queryer.QueryContext(ctx, producerOwedOCIKeysQuery, scopeID, generationID)
	if err != nil {
		return producerOCIKeySet{}, fmt.Errorf("list producer owed OCI keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	keys := producerOCIKeySet{digests: []string{}, pairRepos: []string{}, pairTags: []string{}}
	for rows.Next() {
		var repo, tag, digest sql.NullString
		if err := rows.Scan(&repo, &tag, &digest); err != nil {
			return producerOCIKeySet{}, fmt.Errorf("list producer owed OCI keys: scan: %w", err)
		}
		if digest.Valid {
			keys.digests = append(keys.digests, digest.String)
		}
		if repo.Valid && tag.Valid {
			keys.pairRepos = append(keys.pairRepos, repo.String)
			keys.pairTags = append(keys.pairTags, tag.String)
		}
	}
	if err := rows.Err(); err != nil {
		return producerOCIKeySet{}, fmt.Errorf("list producer owed OCI keys: %w", err)
	}
	return keys, nil
}

// listProducerOwedDriftARNs reads the owed generation's distinct Terraform
// ARNs in one probe. It returns an empty (non-nil) slice when the
// generation carries none, so the listing matches nothing.
func listProducerOwedDriftARNs(ctx context.Context, queryer db.Queryer, scopeID, generationID string) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, producerOwedDriftARNsQuery, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("list producer owed drift ARNs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	arns := []string{}
	for rows.Next() {
		var arn sql.NullString
		if err := rows.Scan(&arn); err != nil {
			return nil, fmt.Errorf("list producer owed drift ARNs: scan: %w", err)
		}
		if arn.Valid {
			arns = append(arns, arn.String)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list producer owed drift ARNs: %w", err)
	}
	return arns, nil
}

// SettleProducerActivations claims every open producer-activation obligation
// and settles each: it reopens the succeeded consumer items that wait on the
// owed producer generation and completes the obligation under the claim
// fence. It reports reopened rows by consumer domain. A second call settles
// nothing: completed obligations stay terminal and the reopen only
// transitions rows still succeeded, so the effect is exactly-once per
// producer activation.
func SettleProducerActivations(ctx context.Context, database *sql.DB) (map[string]int, error) {
	if database == nil {
		return nil, fmt.Errorf("settle producer activations: database is required")
	}
	store := NewIngestionStore(SQLDB{DB: database})
	totals := make(map[string]int)
	for {
		claimed, counts, err := store.SettleOneProducerActivation(ctx, producerSettleOwner, producerSettleLease)
		if err != nil {
			return nil, err
		}
		if !claimed {
			return totals, nil
		}
		for domain, n := range counts {
			totals[domain] += n
		}
	}
}

// producerActivationDB combines the ingestion store's query surface with its
// transaction beginner for the activation Store.
type producerActivationDB struct {
	db.ExecQueryer
	db.Beginner
}

// producerActivationStore returns the activation Store over the ingestion
// store's database, or an error when the store cannot begin transactions.
func (s IngestionStore) producerActivationStore() (activation.Store, error) {
	if s.database == nil || s.beginner == nil {
		return activation.Store{}, fmt.Errorf("settle producer activation: ingestion store db is required")
	}
	return activation.NewStore(producerActivationDB{s.database, s.beginner}), nil
}

// SettleOneProducerActivation claims the oldest open producer-activation
// obligation for owner and settles it. It reports false when nothing was
// claimable. The resolution-engine runner calls Claim and
// SettleClaimedProducerActivation separately so it can run maintenance
// between them; this combines both steps for simple callers.
func (s IngestionStore) SettleOneProducerActivation(ctx context.Context, owner string, lease time.Duration) (bool, map[string]int, error) {
	store, err := s.producerActivationStore()
	if err != nil {
		return false, nil, err
	}
	claimed, err := store.ClaimProducerActivation(ctx, owner, lease)
	if err != nil {
		return false, nil, err
	}
	if claimed == nil {
		return false, nil, nil
	}
	counts, _, err := s.SettleClaimedProducerActivation(ctx, *claimed)
	if err != nil {
		return false, nil, err
	}
	return true, counts, nil
}

// SettleClaimedProducerActivation reopens the succeeded consumer items that
// wait on the claimed obligation's producer generation and completes the
// obligation under the caller's lease fence, in one transaction. A scope
// that moved to another generation retires obsolete; a generation with no
// producer evidence left retires inapplicable. It reports reopened rows by
// consumer domain and the closed settle outcome. The counts are nonzero
// only on a completed outcome: a lost lease rolls the reopen back, so a
// not_owner outcome reports empty counts and the retry counts the rows.
func (s IngestionStore) SettleClaimedProducerActivation(ctx context.Context, work activation.ProducerObligation) (map[string]int, activation.ProducerOutcome, error) {
	store, err := s.producerActivationStore()
	if err != nil {
		return nil, "", err
	}
	settle, begun, err := store.BeginProducerSettle(ctx, work)
	if err != nil || settle == nil {
		if err != nil {
			return nil, "", err
		}
		return map[string]int{}, begun.Outcome, nil
	}
	defer settle.Rollback()
	if !settle.IsActiveGeneration(work.GenerationID) {
		retired, err := settle.Retire(ctx, activation.ProducerObsolete)
		if err != nil {
			return nil, "", err
		}
		return map[string]int{}, retired.Outcome, nil
	}
	carries, err := GenerationCarriesProducerEvidence(ctx, settle.Tx(), work.ScopeID, work.GenerationID)
	if err != nil {
		return nil, "", err
	}
	if !carries {
		retired, err := settle.Retire(ctx, activation.ProducerInapplicable)
		if err != nil {
			return nil, "", err
		}
		return map[string]int{}, retired.Outcome, nil
	}
	// The owed keys are invariant for the settle: fetch each family once,
	// then run each domain listing against the arrays.
	ociKeys, err := listProducerOwedOCIKeys(ctx, settle.Tx(), work.ScopeID, work.GenerationID)
	if err != nil {
		return nil, "", err
	}
	driftARNs, err := listProducerOwedDriftARNs(ctx, settle.Tx(), work.ScopeID, work.GenerationID)
	if err != nil {
		return nil, "", err
	}
	dependents := []struct {
		domain reducer.Domain
		query  string
		args   []any
	}{
		{
			reducer.DomainContainerImageIdentity, listProducerDependentOCIItemsQuery,
			[]any{work.CreatedAt, ociKeys.digests, ociKeys.pairRepos, ociKeys.pairTags},
		},
		{
			reducer.DomainKubernetesCorrelationMaterialization, listProducerDependentOCIItemsQuery,
			[]any{work.CreatedAt, ociKeys.digests, ociKeys.pairRepos, ociKeys.pairTags},
		},
		{
			reducer.DomainAWSCloudRuntimeDrift, listProducerDependentDriftItemsQuery,
			[]any{work.CreatedAt, driftARNs},
		},
	}
	counts := make(map[string]int)
	queue := ReducerQueue{database: settle.Tx(), Now: s.Now}
	for _, dependent := range dependents {
		ids, err := listProducerDependentItemIDs(ctx, settle.Tx(), dependent.query,
			string(dependent.domain), dependent.args...)
		if err != nil {
			return nil, "", err
		}
		for _, id := range ids {
			reopened, err := queue.ReopenSucceeded(ctx, id)
			if err != nil {
				return nil, "", fmt.Errorf("reopen %s producer dependents: %w", dependent.domain, err)
			}
			// Count transitions, not listings: an item a concurrent
			// epoch pass already reopened must not count here.
			if reopened {
				counts[string(dependent.domain)]++
			}
		}
	}
	owned, err := settle.StillOwned(ctx)
	if err != nil {
		return nil, "", err
	}
	if !owned {
		return nil, "", fmt.Errorf("settle claimed producer activation: reopen: %w", ErrProducerLeaseLost)
	}
	total := 0
	for _, n := range counts {
		total += n
	}
	completed, err := settle.Complete(ctx, total)
	if err != nil {
		return nil, "", err
	}
	return committedSettleCounts(counts, completed.Outcome), completed.Outcome, nil
}

// committedSettleCounts reports the reopen counts for a closed settle. Only
// a completed settle committed its reopen; any other outcome rolled back,
// so it reports empty counts and the retry counts the rows.
func committedSettleCounts(counts map[string]int, outcome activation.ProducerOutcome) map[string]int {
	if outcome != activation.ProducerCompleted {
		return map[string]int{}
	}
	return counts
}

// listProducerDependentItemIDs runs one dependency-index listing for the owed
// producer generation. args holds the listing's parameters after the domain:
// the owed-at bound then the owed key arrays.
func listProducerDependentItemIDs(ctx context.Context, queryer db.Queryer, query, domain string, args ...any) ([]string, error) {
	params := append([]any{domain}, args...)
	rows, err := queryer.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, fmt.Errorf("list producer dependent %s items: %w", domain, err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("list producer dependent %s items: scan: %w", domain, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list producer dependent %s items: %w", domain, err)
	}
	return ids, nil
}
