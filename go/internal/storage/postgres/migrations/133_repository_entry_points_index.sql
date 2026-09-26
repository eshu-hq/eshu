-- #7242: repository context reads known Function entry points for one repo.
-- The previous name-trigram bitmap path examined 30,119 candidates and
-- filtered 15,000 rows in a representative synthetic fixture. The query's
-- exact literal predicate matches this partial index; the ordered keys and
-- included language make its result an index-only scan.
--
-- On 300k synthetic entities, five warm samples changed from a 9.632ms
-- median to 0.062ms, with 59 equal rows and zero order mismatches. In a
-- separate 100k fixture, inserting 59 matching rows cost 1.072ms without
-- versus 1.690ms with the index; updating their included language cost
-- 1.660ms versus 1.829ms. These are fixture results, not production p95.
--
-- Build concurrently to preserve content writers. Keep this as the only SQL
-- statement in the file. The bootstrap runner drops an invalid concurrent
-- index before retrying; IF NOT EXISTS by itself would silently skip one.
CREATE INDEX CONCURRENTLY IF NOT EXISTS content_entities_repository_entry_point_idx
    ON content_entities (repo_id, entity_name, relative_path) INCLUDE (language)
    WHERE entity_type = 'Function'
      AND entity_name IN (
          'main', 'handler', 'app', 'create_app', 'lambda_handler',
          'Main', 'Handler', 'App', 'CreateApp', 'LambdaHandler'
      );
