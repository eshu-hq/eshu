// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"math"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// ssmHostInventoryDefaultLimit bounds one host page when the caller names no
// limit. Hosts are the page unit; observation rows follow their host.
const ssmHostInventoryDefaultLimit = 10

// ssmHostInventoryMaxApplications caps the served application list per host.
// A host inventory capture can hold thousands of rows; the cap keeps the
// response bounded and ApplicationsTruncated says when it bit.
const ssmHostInventoryMaxApplications = 100

// ssmHostInventoryMaxFactRowsPerHost caps the admitted-fact window assessed
// per host. A coherent capture within the collector ceilings never reaches
// it; a host that does reads unknown with FactsTruncated set, never a
// coherence verdict on a truncated stream.
const ssmHostInventoryMaxFactRowsPerHost = 6000

// ssmHostInventoryFilter scopes one SSM host inventory read. AllScopes with
// an empty allowlist reads every scope the deployment serves; otherwise scope
// and repository grants bound the read exactly like the cloud inventory
// readback.
type ssmHostInventoryFilter struct {
	AllScopes            bool
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
	InstanceID           string
	AccountID            string
	Region               string
	Status               string
	Limit                int
	Offset               int
}

// ssmAdmittedFact is one admitted SSM host fact envelope reduced to the read
// inputs: identity, generation placement, tombstone state, and payload.
type ssmAdmittedFact struct {
	FactID       string
	FactKind     string
	ScopeID      string
	GenerationID string
	Payload      map[string]any
	Tombstone    bool
}

// ssmHostFactsStore reads admitted SSM host facts from the durable fact
// store. ContentReader implements it; the read entrypoint takes the narrow
// interface so unit tests supply fixture-backed facts without a database.
type ssmHostFactsStore interface {
	SSMHostActiveFacts(ctx context.Context, filter ssmHostInventoryFilter) ([]ssmAdmittedFact, error)
}

// SSMHostActiveFacts returns the active generation's admitted SSM host facts
// for the filter: the status-overlay input the read model assesses per host.
// Rows arrive newest-host-first in a bounded per-host window; a host whose
// window exhausts is flagged truncated by the assembler, never silently
// short-read.
func (cr *ContentReader) SSMHostActiveFacts(ctx context.Context, filter ssmHostInventoryFilter) ([]ssmAdmittedFact, error) {
	if cr == nil || cr.db == nil {
		return nil, nil
	}
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_ssm_host_active_facts"),
			attribute.String("db.sql.table", "fact_records"),
		),
	)
	defer span.End()

	sqlQuery, args := buildSSMHostActiveFactsSQL(filter)
	rows, err := cr.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("query ssm host active facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var facts []ssmAdmittedFact
	for rows.Next() {
		envelope, err := scanJSONPayload(rows)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("query ssm host active facts: %w", err)
		}
		facts = append(facts, ssmAdmittedFactFromEnvelope(envelope))
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("query ssm host active facts: %w", err)
	}
	return facts, nil
}

// ssmAdmittedFactFromEnvelope reduces one jsonb envelope row to the read
// inputs. A malformed row keeps its fact identity with an empty payload so
// the assembler counts it as malformed rather than failing the whole read.
func ssmAdmittedFactFromEnvelope(envelope map[string]any) ssmAdmittedFact {
	payload, _ := envelope["payload"].(map[string]any)
	if payload == nil {
		payload = map[string]any{}
	}
	tombstone, _ := envelope["is_tombstone"].(bool)
	return ssmAdmittedFact{
		FactID:       stringFromMap(envelope, "fact_id"),
		FactKind:     stringFromMap(envelope, "fact_kind"),
		ScopeID:      stringFromMap(envelope, "scope_id"),
		GenerationID: stringFromMap(envelope, "generation_id"),
		Payload:      payload,
		Tombstone:    tombstone,
	}
}

// buildSSMHostActiveFactsSQL assembles the bounded active-generation SSM
// fact read. Only the scopes' active generations are read: the overlay
// assesses the latest admitted signal, and preserved prior truth comes from
// the committed graph nodes, never from stale generations. Host tuple
// filters apply to the payload fields; the per-host window keeps one
// elephantine host from starving the page, and the host-rank cap keeps the
// host count to the page prefix (offset+limit+1, the same prefix the
// committed seeds fetch in the same tuple order), so an active-only fleet
// can neither starve the page nor blow up the read.
func buildSSMHostActiveFactsSQL(filter ssmHostInventoryFilter) (string, []any) {
	clauses := []string{
		"fact_records.fact_kind IN ('aws_ssm_host_application', 'aws_ssm_host_platform')",
	}
	var args []any
	addPayloadFilter := func(field, value string) {
		if value == "" {
			return
		}
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("fact_records.payload->>'%s' = $%d", field, len(args)))
	}
	addPayloadFilter("instance_id", filter.InstanceID)
	addPayloadFilter("account_id", filter.AccountID)
	addPayloadFilter("region", filter.Region)
	if !filter.AllScopes {
		args = append(args, array.Of(filter.AllowedRepositoryIDs), array.Of(filter.AllowedScopeIDs))
		clauses = append(clauses, fmt.Sprintf(
			"(fact_records.scope_id = ANY($%d) OR fact_records.scope_id = ANY($%d))",
			len(args)-1, len(args),
		))
	}
	// The window keeps one row past the cap: a host that fills the extra
	// slot exhausted its window, so the assembler flags it truncated and
	// forces unknown instead of verdicting a silently short-read stream.
	// The host-rank cap keeps the first offset+limit+1 hosts in tuple order
	// (the same prefix and order as the committed seeds and the Go merge),
	// so the merged-union page is exact without reading the whole fleet.
	// The budget resolves through the shared page-bounds helper so SQL and
	// Go can never disagree on the prefix.
	_, _, hostBudget, ok := ssmHostPageBounds(filter)
	if !ok {
		hostBudget = math.MaxInt
	}
	args = append(args, ssmHostInventoryMaxFactRowsPerHost+1, hostBudget)
	query := fmt.Sprintf(`
SELECT jsonb_build_object(
    'fact_id', fact_records.fact_id,
    'fact_kind', fact_records.fact_kind,
    'scope_id', fact_records.scope_id,
    'generation_id', fact_records.generation_id,
    'is_tombstone', fact_records.is_tombstone,
    'payload', fact_records.payload
) AS envelope
FROM (
    SELECT fact_records.*,
        ROW_NUMBER() OVER (
            PARTITION BY fact_records.payload->>'partition', fact_records.payload->>'account_id', fact_records.payload->>'region', fact_records.payload->>'instance_id'
            ORDER BY fact_records.fact_id
        ) AS host_row_number,
        DENSE_RANK() OVER (
            ORDER BY fact_records.payload->>'partition', fact_records.payload->>'account_id', fact_records.payload->>'region', fact_records.payload->>'instance_id'
        ) AS host_rank
    FROM fact_records
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact_records.scope_id
     AND scope.active_generation_id = fact_records.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact_records.scope_id
     AND generation.generation_id = fact_records.generation_id
     AND generation.status = 'active'
    WHERE %s
) AS fact_records
WHERE fact_records.host_row_number <= $%d
  AND fact_records.host_rank <= $%d
ORDER BY fact_records.payload->>'partition', fact_records.payload->>'account_id', fact_records.payload->>'region', fact_records.payload->>'instance_id', fact_records.fact_id
`, strings.Join(clauses, " AND "), len(args)-1, len(args))
	return query, args
}
