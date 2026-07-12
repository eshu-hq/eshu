SELECT COALESCE(
	jsonb_agg(
		jsonb_build_object(
			'kind', fact_kind,
			'schema_version', schema_version,
			'stable_key', stable_fact_key,
			'source_confidence', source_confidence,
			'source_ref', jsonb_build_object(
				'source_system', source_system,
				'scope_id', scope_id,
				'generation_id', generation_id,
				'fact_key', source_fact_key,
				'uri', COALESCE(source_uri, ''),
				'record_id', COALESCE(source_record_id, '')
			),
			'payload', payload
		)
		ORDER BY fact_kind, stable_fact_key
	)::text,
	'[]'
)
FROM fact_records
WHERE scope_id=:'scope_id' AND generation_id=:'generation_id' AND fact_kind LIKE :'component_like';
