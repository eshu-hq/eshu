// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const secretsIAMTrustChainMaxExpansionPasses = 4

const listActiveSecretsIAMTrustChainFactsQuery = `
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
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = ANY($1::text[])
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
      fact.payload->>'service_account_join_key' = ANY($2::text[])
      OR fact.payload->'bound_service_account_join_keys' ?| $2::text[]
      OR fact.payload->>'role_arn' = ANY($3::text[])
      OR fact.payload->>'principal_arn' = ANY($3::text[])
      OR fact.payload->>'web_identity_subject_fingerprint' = ANY($4::text[])
      OR fact.payload->'web_identity_subject_fingerprints' ?| $4::text[]
      OR fact.payload->>'policy_join_key' = ANY($5::text[])
      OR fact.payload->'token_policy_join_keys' ?| $5::text[]
      OR fact.payload->>'kv_path_fingerprint' = ANY($6::text[])
      OR fact.payload->>'principal_fingerprint' = ANY($7::text[])
      OR fact.payload->>'target_principal_fingerprint' = ANY($7::text[])
      OR fact.payload->>'gcp_service_account_email_digest' = ANY($8::text[])
      OR fact.payload->>'target_service_account_email_digest' = ANY($8::text[])
      OR fact.payload->>'gcp_workload_identity_subject_fingerprint' = ANY($4::text[])
  )
  AND (
    $9::timestamptz IS NULL
    OR (fact.observed_at, fact.fact_id) > ($9::timestamptz, $10::text)
  )
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $11
`

// LoadSecretsIAMTrustChainEvidence loads a redaction-safe source packet for the
// secrets/IAM trust-chain reducer. It starts from the triggering
// scope/generation and expands only through explicit join anchors across active
// generations.
func (s FactStore) LoadSecretsIAMTrustChainEvidence(
	ctx context.Context,
	intent reducer.Intent,
) ([]facts.Envelope, reducer.SecretsIAMTrustChainLoadStats, error) {
	if s.db == nil {
		return nil, reducer.SecretsIAMTrustChainLoadStats{}, fmt.Errorf("fact store database is required")
	}
	seed, err := s.ListFactsByKind(ctx, intent.ScopeID, intent.GenerationID, facts.SecretsIAMFactKinds())
	if err != nil {
		return nil, reducer.SecretsIAMTrustChainLoadStats{}, err
	}
	envelopes := make([]facts.Envelope, 0, len(seed))
	seen := map[string]struct{}{}
	for _, envelope := range seed {
		if envelope.IsTombstone {
			continue
		}
		envelopes = appendUniqueSecretsIAMEnvelope(envelopes, seen, envelope)
	}
	anchors, err := secretsIAMTrustChainAnchorsFromEnvelopes(envelopes)
	if err != nil {
		return nil, reducer.SecretsIAMTrustChainLoadStats{}, err
	}
	truncated := false
	for pass := 0; pass < secretsIAMTrustChainMaxExpansionPasses && anchors.hasAny(); pass++ {
		page, err := s.listActiveSecretsIAMTrustChainFacts(ctx, anchors)
		if err != nil {
			return nil, reducer.SecretsIAMTrustChainLoadStats{}, err
		}
		before := len(envelopes)
		for _, envelope := range page {
			envelopes = appendUniqueSecretsIAMEnvelope(envelopes, seen, envelope)
		}
		if len(envelopes) == before {
			break
		}
		anchors, err = secretsIAMTrustChainAnchorsFromEnvelopes(envelopes)
		if err != nil {
			return nil, reducer.SecretsIAMTrustChainLoadStats{}, err
		}
		if pass == secretsIAMTrustChainMaxExpansionPasses-1 && anchors.hasAny() {
			truncated = true
		}
	}
	return envelopes, reducer.SecretsIAMTrustChainLoadStats{
		SeedFactCount:   len(seed),
		LoadedFactCount: len(envelopes),
		Truncated:       truncated,
	}, nil
}

func (s FactStore) listActiveSecretsIAMTrustChainFacts(
	ctx context.Context,
	anchors secretsIAMTrustChainAnchors,
) ([]facts.Envelope, error) {
	var loaded []facts.Envelope
	var cursorObservedAt *time.Time
	var cursorFactID string
	for {
		page, err := s.listActiveSecretsIAMTrustChainFactsPage(ctx, anchors, cursorObservedAt, cursorFactID)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, page...)
		if len(page) < listFactsByKindPageSize {
			return loaded, nil
		}
		last := page[len(page)-1]
		observedAt := last.ObservedAt.UTC()
		cursorObservedAt = &observedAt
		cursorFactID = last.FactID
	}
}

func (s FactStore) listActiveSecretsIAMTrustChainFactsPage(
	ctx context.Context,
	anchors secretsIAMTrustChainAnchors,
	cursorObservedAt *time.Time,
	cursorFactID string,
) ([]facts.Envelope, error) {
	var cursor any
	if cursorObservedAt != nil {
		cursor = cursorObservedAt.UTC()
	}
	rows, err := s.db.QueryContext(
		ctx,
		listActiveSecretsIAMTrustChainFactsQuery,
		facts.SecretsIAMFactKinds(),
		anchors.serviceAccountJoinKeys.values(),
		anchors.roleARNs.values(),
		anchors.webIdentitySubjectFingerprints.values(),
		anchors.vaultPolicyJoinKeys.values(),
		anchors.vaultKVPathFingerprints.values(),
		anchors.gcpPrincipalFingerprints.values(),
		anchors.gcpServiceAccountEmailDigests.values(),
		cursor,
		cursorFactID,
		listFactsByKindPageSize,
	)
	if err != nil {
		return nil, fmt.Errorf("list active secrets/IAM trust-chain facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	loaded := make([]facts.Envelope, 0, listFactsByKindPageSize)
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list active secrets/IAM trust-chain facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active secrets/IAM trust-chain facts: %w", err)
	}
	return loaded, nil
}

type secretsIAMTrustChainAnchors struct {
	serviceAccountJoinKeys         stringSet
	roleARNs                       stringSet
	webIdentitySubjectFingerprints stringSet
	vaultPolicyJoinKeys            stringSet
	vaultKVPathFingerprints        stringSet
	gcpPrincipalFingerprints       stringSet
	gcpServiceAccountEmailDigests  stringSet
}

func secretsIAMTrustChainAnchorsFromEnvelopes(envelopes []facts.Envelope) (secretsIAMTrustChainAnchors, error) {
	anchors := secretsIAMTrustChainAnchors{
		serviceAccountJoinKeys:         stringSet{},
		roleARNs:                       stringSet{},
		webIdentitySubjectFingerprints: stringSet{},
		vaultPolicyJoinKeys:            stringSet{},
		vaultKVPathFingerprints:        stringSet{},
		gcpPrincipalFingerprints:       stringSet{},
		gcpServiceAccountEmailDigests:  stringSet{},
	}
	for _, envelope := range envelopes {
		if err := anchors.addEnvelope(envelope); err != nil {
			return secretsIAMTrustChainAnchors{}, err
		}
	}
	return anchors, nil
}

func (a secretsIAMTrustChainAnchors) hasAny() bool {
	return len(a.serviceAccountJoinKeys) > 0 ||
		len(a.roleARNs) > 0 ||
		len(a.webIdentitySubjectFingerprints) > 0 ||
		len(a.vaultPolicyJoinKeys) > 0 ||
		len(a.vaultKVPathFingerprints) > 0 ||
		len(a.gcpPrincipalFingerprints) > 0 ||
		len(a.gcpServiceAccountEmailDigests) > 0
}

func appendUniqueSecretsIAMEnvelope(
	envelopes []facts.Envelope,
	seen map[string]struct{},
	envelope facts.Envelope,
) []facts.Envelope {
	factID := strings.TrimSpace(envelope.FactID)
	if factID == "" {
		return envelopes
	}
	if _, ok := seen[factID]; ok {
		return envelopes
	}
	seen[factID] = struct{}{}
	return append(envelopes, envelope)
}

type stringSet map[string]struct{}

func (s stringSet) add(value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		s[value] = struct{}{}
	}
}

func (s stringSet) addAll(values []string) {
	for _, value := range values {
		s.add(value)
	}
}

func (s stringSet) values() []string {
	out := make([]string, 0, len(s))
	for value := range s {
		out = append(out, value)
	}
	return out
}
