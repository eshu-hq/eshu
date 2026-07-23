// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/lib/pq"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// cloudInventoryFactKind is the reducer-owned canonical CloudResource identity
// fact kind written by the cloud-inventory admission path. The readback reads
// exactly this kind; it never aggregates provider source facts directly, so the
// answer is always reducer-resolved canonical truth rather than raw provider
// observation.
const cloudInventoryFactKind = "reducer_cloud_resource_identity"

// cloudInventoryIdentities returns canonical CloudResource identity rows from
// each scope's active generation, filtered by the bounded readback filters and
// ordered deterministically. It fetches limit+1 rows so the handler can report
// a continuation offset without a second count query.
func (cr *ContentReader) cloudInventoryIdentities(
	ctx context.Context,
	filter cloudInventoryFilter,
) (cloudInventoryListReadModel, error) {
	if cr == nil || cr.db == nil {
		return cloudInventoryListReadModel{}, nil
	}
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_cloud_inventory_identities"),
			attribute.String("db.sql.table", "fact_records"),
		),
	)
	defer span.End()

	query, args := buildCloudInventoryIdentitiesSQL(filter)
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return cloudInventoryListReadModel{}, fmt.Errorf("query cloud inventory identities: %w", err)
	}
	defer func() { _ = rows.Close() }()

	limit := filter.Limit
	if limit <= 0 {
		limit = cloudInventoryReadbackDefaultLimit
	}
	resources := make([]map[string]any, 0, limit)
	for rows.Next() {
		payload, err := scanJSONPayload(rows)
		if err != nil {
			span.RecordError(err)
			return cloudInventoryListReadModel{}, fmt.Errorf("query cloud inventory identities: %w", err)
		}
		resources = append(resources, payload)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return cloudInventoryListReadModel{}, fmt.Errorf("query cloud inventory identities: %w", err)
	}
	nextCursor := ""
	if len(resources) > limit {
		resources = resources[:limit]
		nextCursor = strconv.Itoa(filter.Offset + limit)
	}
	return cloudInventoryListReadModel{Resources: resources, NextCursor: nextCursor}, nil
}

// buildCloudInventoryIdentitiesSQL assembles the bounded canonical-identity list
// query and its parameters. It anchors on the reducer-owned fact kind, selects
// only the active generation for each ingestion scope, excludes tombstoned
// rows, applies optional payload-scoped equality filters as bound parameters,
// orders deterministically by cloud_resource_uid, and fetches limit+1 rows for
// continuation. The projection returns the envelope metadata plus the
// canonical payload; the handler view drops raw locators.
func buildCloudInventoryIdentitiesSQL(filter cloudInventoryFilter) (string, []any) {
	args := []any{}
	clauses := []string{
		"fact_records.fact_kind = '" + cloudInventoryFactKind + "'",
		"fact_records.is_tombstone = FALSE",
	}
	addPayloadFilter := func(field string, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("fact_records.payload->>'%s' = $%d", field, len(args)))
	}
	addPayloadFilter("provider", filter.Provider)
	addPayloadFilter("management_origin", filter.ManagementOrigin)
	if scope := strings.TrimSpace(filter.ScopeID); scope != "" {
		args = append(args, scope)
		clauses = append(clauses, fmt.Sprintf(
			"(fact_records.scope_id = $%d OR fact_records.payload->>'scope_id' = $%d)",
			len(args), len(args),
		))
	}
	if !filter.AllScopes {
		args = append(args, pq.Array(filter.AllowedRepositoryIDs), pq.Array(filter.AllowedScopeIDs))
		clauses = append(clauses, fmt.Sprintf(
			"(fact_records.scope_id = ANY($%d) OR fact_records.scope_id = ANY($%d))",
			len(args)-1, len(args),
		))
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = cloudInventoryReadbackDefaultLimit
	}
	args = append(args, limit+1, filter.Offset)
	query := fmt.Sprintf(`
SELECT jsonb_build_object(
    'fact_id', fact_records.fact_id,
    'fact_kind', fact_records.fact_kind,
    'scope_id', fact_records.scope_id,
    'generation_id', fact_records.generation_id,
    'source_system', fact_records.source_system,
    'observed_at', fact_records.observed_at,
    'payload', fact_records.payload
) AS payload
FROM fact_records
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact_records.scope_id
 AND scope.active_generation_id = fact_records.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact_records.scope_id
 AND generation.generation_id = fact_records.generation_id
 AND generation.status = 'active'
WHERE %s
ORDER BY fact_records.payload->>'cloud_resource_uid', fact_records.fact_id
LIMIT $%d OFFSET $%d
`, strings.Join(clauses, " AND "), len(args)-1, len(args))
	return query, args
}

// cloudInventoryResourceView projects one canonical identity envelope into the
// bounded wire shape. It reads only reducer-resolved canonical fields from the
// nested payload and intentionally omits raw_identity and any provider locator,
// raw tag value, or credential field so the readback never leaks source-side
// secrets. Tag value fingerprints (keyed, non-reversible markers) are surfaced
// when present so callers can correlate resources by shared tag value without
// the tag value text ever crossing the wire. The provider-neutral source_state
// is derived from management_origin per the multi-cloud collector contract
// Query Truth section.
func cloudInventoryResourceView(envelope map[string]any) map[string]any {
	payload, _ := envelope["payload"].(map[string]any)
	if payload == nil {
		payload = map[string]any{}
	}
	managementOrigin := stringFromMap(payload, "management_origin")
	view := map[string]any{
		"cloud_resource_uid": stringFromMap(payload, "cloud_resource_uid"),
		"provider":           stringFromMap(payload, "provider"),
		"resource_type":      stringFromMap(payload, "resource_type"),
		"management_origin":  managementOrigin,
		"scope_id":           cloudInventoryScopeID(envelope, payload),
		"generation_id":      stringFromMap(envelope, "generation_id"),
		"source_state":       string(cloudInventorySourceState(managementOrigin)),
		"evidence": map[string]any{
			"declared": boolFromMap(payload, "has_declared_evidence"),
			"applied":  boolFromMap(payload, "has_applied_evidence"),
			"observed": boolFromMap(payload, "has_observed_evidence"),
		},
	}
	if fingerprints := cloudInventoryTagFingerprints(payload); len(fingerprints) > 0 {
		view["tag_value_fingerprints"] = fingerprints
	}
	if attrs := cloudInventoryAttributes(payload); len(attrs) > 0 {
		view["attributes"] = attrs
	}
	if evidence := cloudInventoryIdentityPolicyEvidence(payload); len(evidence) > 0 {
		view["identity_policy_evidence"] = evidence
	}
	if boolFromMap(payload, "identity_policy_evidence_truncated") {
		view["identity_policy_evidence_truncated"] = true
	}
	if freshness := cloudInventoryResourceChangeFreshness(payload); len(freshness) > 0 {
		view["resource_change_freshness"] = freshness
	}
	if boolFromMap(payload, "resource_change_freshness_truncated") {
		view["resource_change_freshness_truncated"] = true
	}
	return view
}

// cloudInventoryContainersAttributeKey is the one nested attribute key this
// projector treats as an array-of-objects instead of an array of strings: the
// AWS ECS "containers" attribute the loader-side allowlist writes (issue
// #5449, go/internal/storage/postgres/cloud_inventory_evidence.go
// awsCloudInventoryAttributeAllowlist.nestedArrayKeys["containers"]).
const cloudInventoryContainersAttributeKey = "containers"

// cloudInventoryContainerAttributeKeys is the closed set of container map
// sub-keys this projector surfaces. It MUST stay in lockstep with the AWS
// loader's containers sub-key allowlist
// (go/internal/storage/postgres/cloud_inventory_evidence.go
// awsCloudInventoryAttributeAllowlist.nestedArrayKeys["containers"]): the
// loader already drops every other sub-key before the value reaches this
// projector, but this set is a second, independent gate, so a change to one
// without the other is caught by a test rather than becoming a leak.
var cloudInventoryContainerAttributeKeys = map[string]struct{}{
	"image":        {},
	"image_digest": {},
}

// cloudInventoryAttributes projects the bounded provider-specific attributes map
// from the canonical identity payload. It keeps only non-blank string keys (cap
// 64) whose values are string, bool, float64, or []string/[]any-of-strings, plus
// the allowlisted "containers" nested array-of-objects reduced to {image,
// image_digest} per element. Nested maps under any other key, and any other
// value type, are dropped so no malformed payload content can leak unexpected
// structured data through the readback wire.
func cloudInventoryAttributes(payload map[string]any) map[string]any {
	const maxKeys = 64
	object, ok := payload["attributes"].(map[string]any)
	if !ok || len(object) == 0 {
		return nil
	}
	out := make(map[string]any, len(object))
	for key, value := range object {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if len(out) >= maxKeys {
			break
		}
		if key == cloudInventoryContainersAttributeKey {
			if containers := cloudInventoryContainerAttributes(value); len(containers) > 0 {
				out[key] = containers
			}
			continue
		}
		switch v := value.(type) {
		case string:
			out[key] = v
		case bool:
			out[key] = v
		case float64:
			out[key] = v
		case []string:
			if len(v) > 0 {
				out[key] = v
			}
		case []any:
			strs := make([]string, 0, len(v))
			for _, elem := range v {
				if s, ok := elem.(string); ok && strings.TrimSpace(s) != "" {
					strs = append(strs, s)
				}
			}
			if len(strs) > 0 {
				out[key] = strs
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// cloudInventoryContainerAttributes reduces the raw "containers" attribute
// value to a slice of maps holding only cloudInventoryContainerAttributeKeys.
// An element that is not a JSON object, or whose allowlisted sub-keys are all
// absent or blank, is dropped rather than surfaced as an empty map.
func cloudInventoryContainerAttributes(raw any) []map[string]string {
	elements, ok := raw.([]any)
	if !ok || len(elements) == 0 {
		return nil
	}
	out := make([]map[string]string, 0, len(elements))
	for _, element := range elements {
		item, ok := element.(map[string]any)
		if !ok {
			continue
		}
		kept := map[string]string{}
		for subKey := range cloudInventoryContainerAttributeKeys {
			if s, ok := item[subKey].(string); ok {
				trimmed := strings.TrimSpace(s)
				if trimmed != "" {
					kept[subKey] = trimmed
				}
			}
		}
		if len(kept) > 0 {
			out = append(out, kept)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// cloudInventoryTagFingerprints reads the keyed tag value fingerprint map from a
// canonical identity payload, keeping only string markers under non-blank keys.
// The markers are non-reversible, so surfacing them never exposes tag values.
func cloudInventoryTagFingerprints(payload map[string]any) map[string]string {
	object, ok := payload["tag_value_fingerprints"].(map[string]any)
	if !ok || len(object) == 0 {
		return nil
	}
	out := make(map[string]string, len(object))
	for key, value := range object {
		if marker, ok := value.(string); ok && strings.TrimSpace(key) != "" && marker != "" {
			out[key] = marker
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// cloudInventoryIdentityPolicyEvidence reads bounded identity-policy evidence
// from the canonical identity payload. It keeps only closed safe fields:
// evidence key, bounded identity/role classes, and keyed fingerprints. Raw ARM
// identities, raw assignment scopes, and raw principal GUIDs are intentionally
// omitted even if a malformed payload includes them.
func cloudInventoryIdentityPolicyEvidence(payload map[string]any) []map[string]string {
	rows, ok := payload["identity_policy_evidence"].([]any)
	if !ok || len(rows) == 0 {
		return nil
	}
	out := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		object, ok := row.(map[string]any)
		if !ok {
			continue
		}
		projected := map[string]string{}
		for _, key := range []string{
			"evidence_key",
			"identity_type",
			"role_class",
			"principal_fingerprint",
			"client_fingerprint",
			"object_fingerprint",
			"tenant_fingerprint",
		} {
			if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
				projected[key] = value
			}
		}
		if len(projected) > 0 {
			out = append(out, projected)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// cloudInventoryResourceChangeFreshness reads sanitized provider
// resource-change freshness rows from the canonical identity payload. It
// allowlists fields the reducer wrote and drops provider locators, raw actor
// ids, before/after values, and provider bodies even if an older payload
// contained them.
func cloudInventoryResourceChangeFreshness(payload map[string]any) []map[string]any {
	rows, ok := payload["resource_change_freshness"].([]any)
	if !ok || len(rows) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		object, ok := row.(map[string]any)
		if !ok {
			continue
		}
		safe := map[string]any{
			"evidence_key":               stringFromMap(object, "evidence_key"),
			"change_type":                stringFromMap(object, "change_type"),
			"change_time":                stringFromMap(object, "change_time"),
			"operation":                  stringFromMap(object, "operation"),
			"client_type":                stringFromMap(object, "client_type"),
			"actor_class":                stringFromMap(object, "actor_class"),
			"actor_fingerprint":          stringFromMap(object, "actor_fingerprint"),
			"changed_property_paths":     cloudInventoryStringSlice(object["changed_property_paths"]),
			"changed_property_truncated": boolFromMap(object, "changed_property_truncated"),
			"tombstone_candidate":        boolFromMap(object, "tombstone_candidate"),
		}
		out = append(out, safe)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloudInventoryStringSlice(value any) []string {
	values, ok := value.([]any)
	if !ok || len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if s, ok := value.(string); ok {
			trimmed := strings.TrimSpace(s)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// cloudInventoryScopeID resolves the canonical scope id, preferring the
// fact-record column and falling back to the payload copy the reducer writes.
func cloudInventoryScopeID(envelope, payload map[string]any) string {
	if scope := stringFromMap(envelope, "scope_id"); scope != "" {
		return scope
	}
	return stringFromMap(payload, "scope_id")
}

// cloudInventorySourceState maps the reducer management_origin precedence into
// the provider-neutral source-state taxonomy. A declared origin means a declared
// IaC layer and the canonical identity agree, which is exact; applied and
// observed origins are deterministic correlations without full declared proof,
// which are derived. An unrecognized origin is unknown so a future origin value
// can never silently present as confident evidence.
func cloudInventorySourceState(managementOrigin string) ReplatformingSourceState {
	switch strings.TrimSpace(managementOrigin) {
	case cloudInventoryManagementOriginDeclared:
		return ReplatformingSourceStateExact
	case cloudInventoryManagementOriginApplied, cloudInventoryManagementOriginObserved:
		return ReplatformingSourceStateDerived
	default:
		return ReplatformingSourceStateUnknown
	}
}

// boolFromMap reads a boolean payload field, treating a missing or non-boolean
// value as false so an absent evidence flag never reads as present.
func boolFromMap(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}
