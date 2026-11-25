// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"sync/atomic"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/scope"
)

// DeferredScopedFactQueryParams holds the catalog-derived SQL parameters shared by
// every per-scope deferred fact-load query: the $1 non-repo_id LIKE terms and the
// $2 raw repo_id self-exclusion values. They are computed once from the full
// catalog and reused across partitions, so the per-scope fan-out does not rebuild
// them per query. The fields are exported for the root pre-hoist differential
// proof, which binds the identical params to the frozen pre-hoist query shape.
type DeferredScopedFactQueryParams struct {
	NonRepoIDLike      array.StringArray
	RepoIDValues       array.StringArray
	RepoIDReferenceKey array.StringArray
	// NonRepoIDRawAnchors carries the $8 raw alias anchors for the keyed
	// deferred-load arms (issue #10875): the same alias anchors $1 wraps in
	// LIKE terms, without escaping or markers. It is 1:1 with the anchor
	// subset of the $1 terms, never with the markers.
	NonRepoIDRawAnchors array.StringArray
	// remoteURLs is NOT bound to any query. It is a fingerprint-only input
	// (issue #5483 C2): the strict cross-repo Flux resolver
	// (relationships.discoverStructuredFluxEvidence) matches a manifest's
	// spec.url against each catalog repository's normalized RemoteURL by exact
	// equality, so a repository's remote_url change is a catalog-shape change
	// that can flip a Flux DEPLOYS_FROM edge — yet it moves neither
	// nonRepoIDLike (name/slug aliases) nor repoIDValues. Including the sorted
	// normalized RemoteURLs in DeferredCatalogFingerprint makes a remote_url
	// change invalidate the partition memo, exactly as a rename already does,
	// so the deferred backfill re-discovers the manifest's evidence instead of
	// memo-skipping it.
	remoteURLs array.StringArray
}

// BuildDeferredScopedFactQueryParams derives the shared $1/$2 parameters from the
// catalog. It returns ok=false when neither arm has anything to match, signalling
// the caller to skip the fact load entirely: with no anchor and no repo_id value
// no content/file/gcp fact can resolve a catalog target.
func BuildDeferredScopedFactQueryParams(
	catalog []relationships.CatalogEntry,
) (DeferredScopedFactQueryParams, bool) {
	nonRepoIDTerms := backfillNonRepoIDAnchorTerms(catalog)
	repoIDValues := relationships.CatalogRepoIDValues(catalog)
	if len(nonRepoIDTerms) == 0 && len(repoIDValues) == 0 {
		return DeferredScopedFactQueryParams{}, false
	}

	remoteURLs := catalogRemoteURLValues(catalog)

	nonRepoIDLike := buildPayloadAnchorLikeTerms(nonRepoIDTerms)
	nonRepoIDRawAnchors := backfillNonRepoIDAnchorRawTerms(catalog)
	repoIDRaw := make([]string, 0, len(repoIDValues))
	repoIDReferenceKeys := make([]string, 0, len(repoIDValues))
	for _, value := range repoIDValues {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			repoIDRaw = append(repoIDRaw, value)
			repoIDReferenceKeys = append(repoIDReferenceKeys, relationships.CatalogReferenceKey(value))
		}
	}

	// Zero-length arrays are valid: LIKE ANY({}) is false, so an empty arm simply
	// never contributes a match.
	if nonRepoIDLike == nil {
		nonRepoIDLike = []string{}
	}
	if nonRepoIDRawAnchors == nil {
		nonRepoIDRawAnchors = []string{}
	}
	if repoIDRaw == nil {
		repoIDRaw = []string{}
	}
	if repoIDReferenceKeys == nil {
		repoIDReferenceKeys = []string{}
	}

	return DeferredScopedFactQueryParams{
		NonRepoIDLike:       array.StringArray(nonRepoIDLike),
		RepoIDValues:        array.StringArray(repoIDRaw),
		RepoIDReferenceKey:  array.StringArray(repoIDReferenceKeys),
		NonRepoIDRawAnchors: array.StringArray(nonRepoIDRawAnchors),
		remoteURLs:          array.StringArray(remoteURLs),
	}, true
}

// catalogRemoteURLValues returns the non-empty normalized RemoteURLs of the
// catalog entries, lowercased. It feeds only the partition-memo fingerprint
// (issue #5483 C2), never a query bind: the deferred fact-load query matches on
// alias LIKE terms and repo_id values, but the strict Flux cross-repo resolver
// depends on RemoteURL equality, so the fingerprint must observe RemoteURL
// changes too.
func catalogRemoteURLValues(catalog []relationships.CatalogEntry) []string {
	values := make([]string, 0, len(catalog))
	for _, entry := range catalog {
		remoteURL := strings.ToLower(strings.TrimSpace(entry.RemoteURL))
		if remoteURL != "" {
			values = append(values, remoteURL)
		}
	}
	return values
}

// loadDeferredScopedRelationshipFactsForPartition runs the self-exclusion query
// variant (ListDeferredScopedRelationshipFactRecordsQuery) bounded to one
// (scope_id, generation_id) partition (issue #3710). The catalog-derived $1/$2
// parameters are shared across partitions; $3/$4 bind the partition. $6 is the
// scope_id-derived own-repo_id performance hint (scopestore.RepoIDFromScopeID)
// and $5 is the regex built by excluding $6 from params.RepoIDValues
// (BuildDeferredRepoIDRegex); $5 is passed as a NULL sql.NullString when no
// usable alternation exists, which safely disables the fast arm for this
// partition (see the query's doc comment for why an absent/wrong $6 only costs
// performance, never correctness). The DB excludes facts that only match
// because their own repo_id appears as an anchor, while still loading facts
// whose precomputed reference keys contain another catalog repo_id. Facts with
// no precomputed keys retain the original payload fallback.
// deferredRelationshipKeyFallbackProbeSampleRate bounds the steady-state
// cost of the version-1 fallback probe: it runs on the first partition load
// of the process and then every Nth load. Measured on a 2000-fact seed at
// production bind widths, the probe COUNT costs ~270ms against a ~2.8s
// partition load (~10%), so unsampled probing would nearly double the
// hot-path query count for pure telemetry. Sampling keeps the amortized cost
// near zero while the first-line-after-silence signal still fires within N
// loads of a regression. Sampling never stops entirely (rather than caching
// a clean generation) because a later save path that skips sidecars must
// still surface (issue #10971).
const deferredRelationshipKeyFallbackProbeSampleRate = 64

var deferredRelationshipKeyFallbackProbeCalls atomic.Uint64

func loadDeferredScopedRelationshipFactsForPartition(
	ctx context.Context,
	queryer db.Queryer,
	params DeferredScopedFactQueryParams,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	ownRepoID := scopestore.RepoIDFromScopeID(scopeID)
	if deferredRelationshipKeyFallbackProbeCalls.Add(1)%deferredRelationshipKeyFallbackProbeSampleRate == 1 {
		logDeferredRelationshipKeyFallback(ctx, queryer, scopeID, generationID)
	}
	regex, ok := BuildDeferredRepoIDRegex([]string(params.RepoIDValues), ownRepoID)
	repoIDReferenceKeys := DeferredRepoIDReferenceKeys(params.RepoIDValues, params.RepoIDReferenceKey)
	var regexParam sql.NullString
	if ok {
		regexParam = sql.NullString{String: regex, Valid: true}
	}

	// Each partition load runs in its own short transaction carrying the
	// deferred statement timeout and application_name (issue #11460): a
	// regressed partition plan fails loudly at 120s instead of holding its
	// snapshot for the whole pass, and the transaction is gone before the
	// caller merges the partition's facts.
	appName := backfillApplicationName(ctx, BackfillApplicationNameDeferred)
	var loaded []facts.Envelope
	pageErr := runGuardedBackfillQuery(
		ctx, queryer, deferredPartitionStatementTimeout, appName,
		func(q db.Queryer) error {
			rows, err := q.QueryContext(
				ctx,
				ListDeferredScopedRelationshipFactRecordsQuery,
				params.NonRepoIDLike,
				params.RepoIDValues,
				scopeID,
				generationID,
				regexParam,
				ownRepoID,
				repoIDReferenceKeys,
				params.NonRepoIDRawAnchors,
			)
			if err != nil {
				return err
			}
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				envelope, scanErr := ScanFactEnvelope(rows)
				if scanErr != nil {
					return scanErr
				}
				loaded = append(loaded, envelope)
			}
			return rows.Err()
		},
	)
	if pageErr != nil {
		return nil, pageErr
	}
	return loaded, nil
}

// DeferredRelationshipKeyFallbackProbeSQL counts the partition's eligible
// facts still missing a version-1 sidecar row (issue #10875). A nonzero
// count means the legacy text arms fired for this partition; once the
// backfill completes the count must read zero, which is when the legacy
// fallback becomes removable. It is a partition-index-bounded count, not
// part of the load itself. Exported so test fakes can answer it without
// consuming a scripted FIFO response: fixtures model a corpus with no
// sidecar state, which probes as zero missing rows.
const DeferredRelationshipKeyFallbackProbeSQL = `
SELECT count(*)
FROM fact_records AS fact
WHERE fact.scope_id = $1
  AND fact.generation_id = $2
  AND fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
  AND NOT EXISTS (
    SELECT 1 FROM relationship_reference_candidate_keys AS ref
    WHERE ref.fact_id = fact.fact_id
      AND ref.scope_id = fact.scope_id
      AND ref.generation_id = fact.generation_id
      AND ref.keys_version >= 1
  )
`

// logDeferredRelationshipKeyFallback emits one structured line per partition
// load while any eligible fact still lacks a version-1 sidecar row. Silence
// is the steady state: after the backfill completes this logs nothing, and
// the first line after silence names the regression. A probe failure never
// fails the load it observes.
func logDeferredRelationshipKeyFallback(ctx context.Context, queryer db.Queryer, scopeID, generationID string) {
	rows, err := queryer.QueryContext(ctx, DeferredRelationshipKeyFallbackProbeSQL, scopeID, generationID)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	var missing int64
	if rows.Next() {
		if err := rows.Scan(&missing); err != nil {
			return
		}
	}
	if err := rows.Err(); err != nil {
		return
	}
	if missing > 0 {
		log.Printf("event_name=deferred_relationship_key.fallback scope_id=%q generation_id=%q missing_versioned_key_rows=%d", scopeID, generationID, missing)
	}
}

// DeferredRepoIDReferenceKeys resolves the $7 reference-key array for the
// hoisted deferred fact query. Exported for the root pre-hoist differential
// proof, which binds the identical args to the shipped hoisted query shape.
func DeferredRepoIDReferenceKeys(repoIDValues, repoIDReferenceKeys array.StringArray) array.StringArray {
	if len(repoIDReferenceKeys) == len(repoIDValues) {
		return repoIDReferenceKeys
	}
	keys := make([]string, 0, len(repoIDValues))
	for _, value := range repoIDValues {
		keys = append(keys, relationships.CatalogReferenceKey(value))
	}
	return array.StringArray(keys)
}
