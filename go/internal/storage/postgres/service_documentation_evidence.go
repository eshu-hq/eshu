// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// serviceDocumentationEvidenceQuery loads the active, non-tombstoned documentation
// facts that reference one service, returning only the durable external identity
// the docs evidence family (#1988) keys on plus the observable fields it hashes.
//
// It selects the three documentation fact kinds the documentation read model
// surfaces (entity mentions, claim candidates, semantic observations), gates on
// the active scope generation and is_tombstone = FALSE exactly as the read model
// does, and matches the service through the same target-ref containment shapes
// the read model uses (candidate_refs / evidence_refs / linked_entities holding a
// {service} ref). source_system and source_record_id are durable fact_records
// columns; document_id is a durable payload field read from the top-level field
// (documentation entity mentions / claim candidates) or from the nested
// source.document_id (semantic documentation observations, whose SemanticSourceRef
// carries it under source). The generation-bearing fact_id and generation_id are
// deliberately not projected, so the reducer cannot key on them.
//
// source_acl_state is the bounded source-ACL-state observation
// (allowed|denied|partial|missing|stale) the collector emits on the fact's
// acl_summary (facts.DocumentationACLSummary.SourceACLState). It is read verbatim
// and COALESCEd to the empty string when the fact carries no ACL summary, so a
// fact with no observed access-posture signal yields no ACL claim. The reducer
// validates and projects it; this read never upgrades, defaults, or invents a
// value the collector did not assert.
//
// Parameter order:
//
//	$1 service_id (the service whose documentation evidence is loaded)
//	$2 candidate/evidence ref containment ('{"candidate_refs":[{"kind":"service","id":<service_id>}]}')
//	$3 evidence ref containment ('{"evidence_refs":[{"kind":"service","id":<service_id>}]}')
//	$4 linked-entity containment ('{"linked_entities":[{"entity_type":"service","entity_id":<service_id>}]}')
const serviceDocumentationEvidenceQuery = `
SELECT
    fact.source_system,
    COALESCE(fact.source_record_id, ''),
    COALESCE(fact.payload->>'document_id', fact.payload->'source'->>'document_id', ''),
    fact.fact_kind,
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.payload->>'observation_hash', ''),
    COALESCE(fact.payload->'acl_summary'->>'source_acl_state', '')
FROM fact_records AS fact
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind IN (
        'documentation_entity_mention',
        'documentation_claim_candidate',
        'semantic.documentation_observation'
      )
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
        fact.payload @> $2::jsonb
        OR fact.payload @> $3::jsonb
        OR fact.payload @> $4::jsonb
      )
ORDER BY fact.source_system ASC, fact.source_record_id ASC, fact.fact_id ASC
`

// ServiceDocumentationEvidenceLoader loads the documentation facts that reference
// each correlated service so the reducer can materialize the docs evidence family
// (#1988). It implements reducer.ServiceScopedDocumentationEvidenceLoader. The
// load is bounded: one query per service over the active-generation
// documentation fact set, returning only durable external identity.
type ServiceDocumentationEvidenceLoader struct {
	queryer Queryer
}

// NewServiceDocumentationEvidenceLoader constructs a read-only documentation
// evidence loader over the shared query surface.
func NewServiceDocumentationEvidenceLoader(queryer Queryer) ServiceDocumentationEvidenceLoader {
	return ServiceDocumentationEvidenceLoader{queryer: queryer}
}

// GetDocumentationEvidenceForServices loads the active documentation evidence for
// each service id, keyed by service id. A service with no referencing
// documentation facts is omitted from the map (its slice is empty). It is a no-op
// for an empty service set so a generation with no services runs no query.
func (l ServiceDocumentationEvidenceLoader) GetDocumentationEvidenceForServices(
	ctx context.Context,
	serviceIDs []string,
) (map[string][]reducer.ServiceDocumentationRecord, error) {
	if l.queryer == nil {
		return nil, fmt.Errorf("documentation evidence queryer is required")
	}
	out := map[string][]reducer.ServiceDocumentationRecord{}
	for _, serviceID := range serviceIDs {
		serviceID = strings.TrimSpace(serviceID)
		if serviceID == "" {
			continue
		}
		records, err := l.loadForService(ctx, serviceID)
		if err != nil {
			return nil, err
		}
		if len(records) > 0 {
			out[serviceID] = records
		}
	}
	return out, nil
}

func (l ServiceDocumentationEvidenceLoader) loadForService(
	ctx context.Context,
	serviceID string,
) ([]reducer.ServiceDocumentationRecord, error) {
	candidateRef := documentationServiceRefContains("candidate_refs", "kind", "id", serviceID)
	evidenceRef := documentationServiceRefContains("evidence_refs", "kind", "id", serviceID)
	linkedRef := documentationServiceRefContains("linked_entities", "entity_type", "entity_id", serviceID)

	rows, err := l.queryer.QueryContext(
		ctx,
		serviceDocumentationEvidenceQuery,
		serviceID,
		candidateRef,
		evidenceRef,
		linkedRef,
	)
	if err != nil {
		return nil, fmt.Errorf("load service documentation evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	records := make([]reducer.ServiceDocumentationRecord, 0)
	for rows.Next() {
		var record reducer.ServiceDocumentationRecord
		if scanErr := rows.Scan(
			&record.SourceSystem,
			&record.SourceRecordID,
			&record.DocumentID,
			&record.FactKind,
			&record.SourceURI,
			&record.ObservationHash,
			&record.SourceACLState,
		); scanErr != nil {
			return nil, fmt.Errorf("scan service documentation evidence: %w", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load service documentation evidence: %w", err)
	}
	return records, nil
}

// documentationServiceRefContains builds the JSONB containment literal that
// matches a documentation fact referencing the service through a {service} ref in
// the named list. It mirrors the documentationTargetContainsPayloads shapes the
// query-layer documentation read model uses, so the reducer load and the read
// surface agree on what "references this service" means.
func documentationServiceRefContains(listKey, kindKey, idKey, serviceID string) string {
	idLiteral := documentationJSONString(serviceID)
	return fmt.Sprintf(
		`{"%s":[{"%s":"service","%s":%s}]}`,
		listKey,
		kindKey,
		idKey,
		idLiteral,
	)
}

// documentationJSONString encodes a string as a JSON string literal, escaping the
// characters that would otherwise break the containment literal so an exotic
// service id cannot produce malformed JSON.
func documentationJSONString(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
