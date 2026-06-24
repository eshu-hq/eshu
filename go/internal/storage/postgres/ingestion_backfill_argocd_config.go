package postgres

import (
	"context"

	"github.com/lib/pq"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// listArgoCDGeneratorConfigFactRecordsQuery loads the latest-generation
// content/file facts of an ArgoCD ApplicationSet's external git-generator config
// repositories, restricted to the .yaml/.yml/.json paths the generator can match.
//
// It is phase two of the per-commit backfill fact load (issue #3570). The marker
// anchors load the ApplicationSet facts (phase one), but an ApplicationSet's
// synthesized deploy edges depend on config files that live in a DIFFERENT
// repository (the git file generator's repoURL), keyed in DiscoverEvidence's
// content index by the config repo's id. Those config files reference the
// newly-onboarded deploy repo only through template parameters
// (team/service/path basename), so neither the alias anchors nor the ArgoCD
// markers select them. Without them the content index is incomplete and
// argocdEvaluatedTemplateSources / argocdConfigIdentityDeploySources cannot
// synthesize the deploy repoURL, dropping the edge. This query reloads exactly
// those config files so the content index is complete.
//
// The path filter is a provable superset of the files the generator can read:
// isArgoCDGitFileGeneratorPath only accepts .yaml/.yml/.json suffixes, so every
// file argocdGeneratorPathMatches could select has one of them. repo_id ANY($1)
// bounds the load to the resolved config repos, and the latest-generation join
// matches the other relationship-fact loaders.
const listArgoCDGeneratorConfigFactRecordsQuery = latestGenerationCTE + `
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
      )
ORDER BY fact.observed_at ASC, fact.fact_id ASC
`

// loadArgoCDGeneratorConfigFacts loads the latest-generation .yaml/.yml/.json
// content/file facts whose payload repo_id is one of the supplied config repo
// IDs. It returns nil without querying when configRepoIDs is empty, so the
// per-commit backfill skips phase two when no ApplicationSet targets an external
// config repo.
func loadArgoCDGeneratorConfigFacts(
	ctx context.Context,
	queryer Queryer,
	configRepoIDs []string,
) ([]facts.Envelope, error) {
	if queryer == nil || len(configRepoIDs) == 0 {
		return nil, nil
	}

	rows, err := queryer.QueryContext(
		ctx,
		listArgoCDGeneratorConfigFactRecordsQuery,
		pq.StringArray(configRepoIDs),
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var loaded []facts.Envelope
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return loaded, nil
}

// mergeRelationshipFacts appends the secondary facts to the primary facts,
// dropping any secondary fact whose FactID already appears in primary so the
// merged slice (the input to DiscoverEvidence) has no duplicate envelopes. A
// config repo's ApplicationSet fact can appear in both the marker-selected phase
// one and the repo-id-scoped phase two, so de-duplication keeps the content index
// and discovery deterministic. Facts with an empty FactID are always kept because
// they carry no stable identity to de-duplicate on.
func mergeRelationshipFacts(primary, secondary []facts.Envelope) []facts.Envelope {
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
