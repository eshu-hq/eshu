#!/usr/bin/env bash
#
# golden-corpus-dead-code-fixtures.sh — deterministic B-7 cross-repo dead-code
# evidence for the minimal corpus. The source-local pipeline supplies
# python_comprehensive candidates and orders-api as a consumer; this helper adds
# bounded reachability rows after the real drain so the golden query shape
# exercises dead, live_by_consumer, unknown, and suppressed buckets together.

golden_pg_exec() {
	local sql="$1"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -f "${compose_file}" exec -T postgres \
			psql -U eshu -d eshu -v ON_ERROR_STOP=1 -c "${sql}" >/dev/null
	else
		command -v psql >/dev/null 2>&1 || die "psql client required in --no-compose mode"
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${sql}" >/dev/null
	fi
}

seed_cross_repo_dead_code_fixture() {
	golden_pg_exec "
	WITH repo_catalog AS (
	  SELECT scope_id,
	         active_generation_id AS generation_id,
	         COALESCE(payload->>'repo_id', payload->>'id', scope_id) AS repo_id,
	         COALESCE(payload->>'name', payload->>'repo_name', payload->>'repo_slug', scope_id) AS repo_name
	  FROM ingestion_scopes
	  WHERE scope_kind = 'repository'
	),
	consumer_scope AS (
	  SELECT scope_id, generation_id, repo_id
	  FROM repo_catalog
	  WHERE repo_name = 'orders-api'
	),
	producer_scope AS (
	  SELECT repo_id
	  FROM repo_catalog
	  WHERE repo_name = 'python_comprehensive'
	),
	active_consumer_generation AS (
	  SELECT consumer_scope.scope_id, consumer_scope.generation_id, consumer_scope.repo_id
	  FROM consumer_scope
	  JOIN scope_generations AS generation
	    ON generation.generation_id = consumer_scope.generation_id
	   AND generation.status = 'active'
	),
	root AS (
	  SELECT entity_id AS root_entity_id
	  FROM content_entities
	  JOIN active_consumer_generation AS consumer
	    ON content_entities.repo_id = consumer.repo_id
	  WHERE relative_path = 'main.go'
	    AND entity_name = 'main'
	),
		producer AS (
		  SELECT content_entities.entity_name, content_entities.entity_id
		  FROM content_entities
		  JOIN producer_scope AS producer_scope
		    ON content_entities.repo_id = producer_scope.repo_id
		  WHERE relative_path = 'async_code.py'
		    AND content_entities.entity_type = 'Function'
		    AND content_entities.entity_name IN ('process_batch', 'shutdown')
		)
		INSERT INTO code_reachability_rows (
		  scope_id, generation_id, repository_id, root_entity_id, entity_id,
		  depth, state, confidence, min_resolution_method,
		  evidence, root_kinds, observed_at, updated_at
		)
	SELECT consumer.scope_id,
	       consumer.generation_id,
	       consumer.repo_id,
	       root.root_entity_id,
	       producer.entity_id,
	       2,
	       CASE producer.entity_name WHEN 'shutdown' THEN 'ambiguous' ELSE 'reachable' END,
	       CASE producer.entity_name WHEN 'shutdown' THEN 0.5 ELSE 0.99 END,
	       CASE producer.entity_name WHEN 'shutdown' THEN 'repo_unique_name' ELSE 'scip' END,
		       jsonb_build_array('CALLS:' || root.root_entity_id || '->' || producer.entity_id),
	       jsonb_build_array('go.main_function'),
	       now(),
	       now()
		FROM active_consumer_generation AS consumer
		CROSS JOIN root
		JOIN producer ON true
		ON CONFLICT (scope_id, generation_id, repository_id, root_entity_id, entity_id) DO UPDATE
		SET depth = EXCLUDED.depth,
		    state = EXCLUDED.state,
		    confidence = EXCLUDED.confidence,
		    min_resolution_method = EXCLUDED.min_resolution_method,
		    evidence = EXCLUDED.evidence,
		    root_kinds = EXCLUDED.root_kinds,
		    observed_at = EXCLUDED.observed_at,
		    updated_at = EXCLUDED.updated_at;
	"
	golden_pg_exec "
	DO \$\$
	DECLARE
	  seeded_count integer;
	BEGIN
	  WITH repo_catalog AS (
	    SELECT scope_id,
	           active_generation_id AS generation_id,
	           COALESCE(payload->>'repo_id', payload->>'id', scope_id) AS repo_id,
	           COALESCE(payload->>'name', payload->>'repo_name', payload->>'repo_slug', scope_id) AS repo_name
	    FROM ingestion_scopes
	    WHERE scope_kind = 'repository'
	  ),
	  consumer_scope AS (
	    SELECT scope_id, generation_id, repo_id
	    FROM repo_catalog
	    WHERE repo_name = 'orders-api'
	  ),
	  producer_scope AS (
	    SELECT repo_id
	    FROM repo_catalog
	    WHERE repo_name = 'python_comprehensive'
		  ),
		  producer_entities AS (
		    SELECT content_entities.entity_id
		    FROM content_entities
		    JOIN producer_scope
		      ON content_entities.repo_id = producer_scope.repo_id
		    WHERE content_entities.relative_path = 'async_code.py'
		      AND content_entities.entity_type = 'Function'
		      AND content_entities.entity_name IN ('process_batch', 'shutdown')
		  )
	  SELECT COUNT(*)
	  INTO seeded_count
	  FROM code_reachability_rows AS row
	  JOIN consumer_scope
	    ON row.scope_id = consumer_scope.scope_id
	   AND row.generation_id = consumer_scope.generation_id
	   AND row.repository_id = consumer_scope.repo_id
	  WHERE row.entity_id IN (SELECT entity_id FROM producer_entities);
	  IF seeded_count < 2 THEN
	    RAISE EXCEPTION 'expected at least 2 cross-repo dead-code reachability rows, got %', seeded_count;
	  END IF;
	END
	\$\$;
	"
}
