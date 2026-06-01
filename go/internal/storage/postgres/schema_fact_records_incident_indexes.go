package postgres

const incidentFactRecordReadIndexesSQL = `CREATE INDEX IF NOT EXISTS fact_records_incident_context_record_lookup_idx ON fact_records (source_system, (payload->>'provider_incident_id'), scope_id, observed_at DESC, fact_id ASC) WHERE fact_kind = 'incident.record' AND is_tombstone = FALSE;
CREATE INDEX IF NOT EXISTS fact_records_incident_context_timeline_lookup_idx ON fact_records (scope_id, generation_id, (payload->>'provider_incident_id'), (payload->>'created_at'), fact_id ASC) WHERE fact_kind = 'incident.lifecycle_event' AND is_tombstone = FALSE;
CREATE INDEX IF NOT EXISTS fact_records_incident_context_change_services_idx ON fact_records USING GIN (payload jsonb_path_ops) WHERE fact_kind = 'change.record' AND is_tombstone = FALSE;
`
