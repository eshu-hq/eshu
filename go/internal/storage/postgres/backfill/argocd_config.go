// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"errors"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ListArgoCDGeneratorConfigFactRecordsQuery loads the latest-generation
// content/file facts of an ArgoCD ApplicationSet's git-generator config
// repositories, restricted to the .yaml/.yml/.json paths the generator can match.
//
// It is phase two of the per-commit backfill fact load (issue #3570). The
// marker anchors load the ApplicationSet facts (phase one), but an
// ApplicationSet's synthesized deploy edges depend on config files in the git
// file generator's repoURL, which may be another repository or the control
// repository itself (#7778). Either way the markers do not select them. They
// are keyed in DiscoverEvidence's content index by the config repo's id. Those
// config files reference the newly-onboarded deploy repo only through template
// parameters (team/service/path basename), so neither the alias anchors nor
// the ArgoCD markers select them. Without them the content index is incomplete
// and argocdEvaluatedTemplateSources / argocdConfigIdentityDeploySources
// cannot synthesize the deploy repoURL, dropping the edge. This query reloads
// exactly those config files so the content index is complete.
//
// The path filter is a provable superset of the files the generator can read:
// isArgoCDGitFileGeneratorPath only accepts .yaml/.yml/.json suffixes, so every
// file argocdGeneratorPathMatches could select has one of them. repo_id ANY($1)
// bounds the load to the resolved config repos, and the latest-generation join
// matches the other relationship-fact loaders.
// argoCDGeneratorConfigFactSelectBody is the shared SELECT body behind the
// ArgoCD config-repo fact reads. The unpaged query and both keyset pages
// derive from it by appending their own tail, so the three statements cannot
// drift apart (issue #11460).
const argoCDGeneratorConfigFactSelectBody = LatestGenerationCTE + `
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''),
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
JOIN latest_generations AS latest
  ON latest.scope_id = fact.scope_id
 AND latest.generation_id = fact.generation_id
WHERE latest.generation_id IS NOT NULL
  AND fact.fact_kind IN ('content', 'file')
  AND fact.payload->>'repo_id' = ANY($1)
  AND (
        lower(COALESCE(fact.payload->>'content_path', fact.payload->>'relative_path', '')) LIKE '%.yaml'
     OR lower(COALESCE(fact.payload->>'content_path', fact.payload->>'relative_path', '')) LIKE '%.yml'
     OR lower(COALESCE(fact.payload->>'content_path', fact.payload->>'relative_path', '')) LIKE '%.json'
      )`

const ListArgoCDGeneratorConfigFactRecordsQuery = argoCDGeneratorConfigFactSelectBody + `
ORDER BY fact.observed_at ASC, fact.fact_id ASC
`

// ListArgoCDGeneratorConfigFactRecordsFirstPageQuery is the first keyset page
// of the config reload: the shipped query with a bounded LIMIT. Later pages
// use the cursor variant below.
const ListArgoCDGeneratorConfigFactRecordsFirstPageQuery = argoCDGeneratorConfigFactSelectBody + `
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $2
`

// ListArgoCDGeneratorConfigFactRecordsCursorPageQuery is the follow-on keyset
// page: the shipped query narrowed to rows after the ($2, $3) cursor with a
// bounded LIMIT. It stays a separate statement from the first page so the row
// comparison is a plain predicate under a generic plan (the #6154 lesson).
const ListArgoCDGeneratorConfigFactRecordsCursorPageQuery = argoCDGeneratorConfigFactSelectBody + `
AND ((fact.observed_at, fact.fact_id) > ($2, $3))
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $4
`

// LoadArgoCDGeneratorConfigFacts loads the latest-generation .yaml/.yml/.json
// content/file facts whose payload repo_id is one of the supplied config repo
// IDs. It returns nil without querying when configRepoIDs is empty, so the
// per-commit backfill skips phase two when no ApplicationSet targets a config
// repo.
//
// The load pages by the (observed_at, fact_id) keyset with a bounded LIMIT
// (issue #11460): every page runs in its own short transaction carrying the
// caller's statement timeout and application_name, committed before the next
// page begins. A page that dies to statement_timeout — or an overall load
// that outlives its context deadline — fails loudly, never silently
// truncating the config set.
func LoadArgoCDGeneratorConfigFacts(
	ctx context.Context,
	queryer db.Queryer,
	configRepoIDs []string,
) ([]facts.Envelope, error) {
	return loadArgoCDGeneratorConfigFactsPaged(ctx, queryer, configRepoIDs, backfillFactPageSize)
}

// loadArgoCDGeneratorConfigFactsPaged is LoadArgoCDGeneratorConfigFacts with
// an explicit page size, so tests can force multi-page loads.
func loadArgoCDGeneratorConfigFactsPaged(
	ctx context.Context,
	queryer db.Queryer,
	configRepoIDs []string,
	pageSize int,
) ([]facts.Envelope, error) {
	if queryer == nil || len(configRepoIDs) == 0 {
		return nil, nil
	}
	if pageSize <= 0 {
		pageSize = backfillFactPageSize
	}

	ctx, cancel := context.WithTimeout(ctx, anchorScopedOverallTimeout)
	defer cancel()
	appName := backfillApplicationName(ctx, backfillApplicationNameFallback)

	var loaded []facts.Envelope
	var cursorSet bool
	var cursorAt time.Time
	var cursorID string
	for {
		var query string
		var args []any
		if !cursorSet {
			query = ListArgoCDGeneratorConfigFactRecordsFirstPageQuery
			args = []any{array.StringArray(configRepoIDs), pageSize}
		} else {
			query = ListArgoCDGeneratorConfigFactRecordsCursorPageQuery
			args = []any{array.StringArray(configRepoIDs), cursorAt, cursorID, pageSize}
		}
		var page []facts.Envelope
		pageErr := runGuardedBackfillQuery(
			ctx, queryer, anchorScopedStatementTimeout, appName,
			func(q db.Queryer) error {
				rows, err := q.QueryContext(ctx, query, args...)
				if err != nil {
					return err
				}
				defer func() { _ = rows.Close() }()
				for rows.Next() {
					envelope, scanErr := ScanFactEnvelope(rows)
					if scanErr != nil {
						return scanErr
					}
					page = append(page, envelope)
				}
				return rows.Err()
			},
		)
		if pageErr != nil {
			if ctx.Err() != nil {
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					return nil, backfillTimeoutExceeded(
						"argocd generator config fact load", anchorScopedOverallTimeout, pageErr,
					)
				}
				return nil, pageErr
			}
			if isBackfillTimeoutError(pageErr) {
				return nil, backfillTimeoutExceeded(
					"argocd generator config fact load", anchorScopedStatementTimeout, pageErr,
				)
			}
			return nil, pageErr
		}
		loaded = append(loaded, page...)
		if len(page) < pageSize {
			return loaded, nil
		}
		last := page[len(page)-1]
		cursorAt, cursorID, cursorSet = last.ObservedAt, last.FactID, true
	}
}

// MergeRelationshipFacts appends the secondary facts to the primary facts,
// dropping any secondary fact whose FactID already appears in primary so the
// merged slice (the input to DiscoverEvidence) has no duplicate envelopes. A
// config repo's ApplicationSet fact can appear in both the marker-selected phase
// one and the repo-id-scoped phase two, so de-duplication keeps the content index
// and discovery deterministic. Facts with an empty FactID are always kept because
// they carry no stable identity to de-duplicate on.
func MergeRelationshipFacts(primary, secondary []facts.Envelope) []facts.Envelope {
	if len(secondary) == 0 {
		return primary
	}
	seen := make(map[string]struct{}, len(primary))
	for _, envelope := range primary {
		if envelope.FactID != "" {
			seen[envelope.FactID] = struct{}{}
		}
	}
	merged := make([]facts.Envelope, 0, len(primary)+len(secondary))
	merged = append(merged, primary...)
	for _, envelope := range secondary {
		if envelope.FactID != "" {
			if _, ok := seen[envelope.FactID]; ok {
				continue
			}
			seen[envelope.FactID] = struct{}{}
		}
		merged = append(merged, envelope)
	}
	return merged
}
