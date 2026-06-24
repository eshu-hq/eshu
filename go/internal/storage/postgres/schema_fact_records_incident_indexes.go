// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const incidentFactRecordReadIndexesSQL = `CREATE INDEX IF NOT EXISTS fact_records_incident_context_record_lookup_idx ON fact_records (source_system, (payload->>'provider_incident_id'), scope_id, observed_at DESC, fact_id ASC) WHERE fact_kind = 'incident.record' AND is_tombstone = FALSE;
CREATE INDEX IF NOT EXISTS fact_records_incident_context_record_source_record_idx ON fact_records (source_system, source_record_id, scope_id, observed_at DESC, fact_id ASC) WHERE fact_kind = 'incident.record' AND is_tombstone = FALSE AND source_record_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS fact_records_incident_context_timeline_lookup_idx ON fact_records (scope_id, generation_id, (payload->>'provider_incident_id'), (payload->>'created_at'), fact_id ASC) WHERE fact_kind = 'incident.lifecycle_event' AND is_tombstone = FALSE;
CREATE INDEX IF NOT EXISTS fact_records_incident_context_change_services_idx ON fact_records USING GIN (payload jsonb_path_ops) WHERE fact_kind = 'change.record' AND is_tombstone = FALSE;
CREATE INDEX IF NOT EXISTS fact_records_incident_repository_correlation_service_idx ON fact_records ((payload->>'repository_id'), (payload->>'provider'), (payload->>'provider_service_id'), fact_id ASC, generation_id) WHERE fact_kind = 'reducer_incident_repository_correlation' AND is_tombstone = FALSE;
CREATE INDEX IF NOT EXISTS fact_records_incident_record_provider_service_idx ON fact_records ((COALESCE(payload->'service'->>'id', payload->>'service_id', '')), (payload->>'provider'), fact_id ASC, generation_id) WHERE fact_kind = 'incident.record' AND is_tombstone = FALSE;
CREATE INDEX IF NOT EXISTS fact_records_incident_routing_applied_service_idx ON fact_records ((payload->>'provider_object_id'), stable_fact_key, fact_id ASC, generation_id) WHERE fact_kind = 'incident_routing.applied_pagerduty_resource' AND is_tombstone = FALSE AND payload->>'resource_class' = 'service';
CREATE INDEX IF NOT EXISTS fact_records_incident_routing_observed_service_idx ON fact_records ((COALESCE(NULLIF(payload->>'provider_object_id', ''), payload->>'service_id', '')), stable_fact_key, fact_id ASC, generation_id) WHERE fact_kind = 'incident_routing.observed_pagerduty_service' AND is_tombstone = FALSE;
`
