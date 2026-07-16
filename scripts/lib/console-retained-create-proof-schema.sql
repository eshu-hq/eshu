BEGIN;
SELECT format('CREATE SCHEMA %I', :'proof_schema') \gexec
SELECT set_config('eshu.proof_schema', :'proof_schema', false);
CREATE TEMP TABLE proof_auth_tables(table_name text PRIMARY KEY);
INSERT INTO proof_auth_tables(table_name)
SELECT table_name
FROM information_schema.tables
WHERE table_schema = 'public'
  AND (
    table_name LIKE 'identity_%'
    OR table_name IN (
      'browser_sessions',
      'governance_audit_events',
      'tenant_repository_grants',
      'tenant_scope_grants',
      'tenants',
      'workspaces'
    )
  )
ORDER BY table_name;

DO $proof$
DECLARE
  target_schema text := current_setting('eshu.proof_schema');
  target_table text;
BEGIN
  EXECUTE format(
    'CREATE TABLE %I._public_identity_snapshots (table_name text PRIMARY KEY, row_count bigint NOT NULL, row_digest text NOT NULL)',
    target_schema
  );
  FOR target_table IN SELECT table_name FROM proof_auth_tables ORDER BY table_name LOOP
    EXECUTE format(
      'CREATE TABLE %I.%I (LIKE public.%I INCLUDING ALL)',
      target_schema,
      target_table,
      target_table
    );
    EXECUTE format(
      'INSERT INTO %I._public_identity_snapshots '
      'SELECT %L, count(*), COALESCE(md5(string_agg(row_json, chr(10) ORDER BY row_json)), md5('''')) '
      'FROM (SELECT to_jsonb(source_row)::text AS row_json FROM public.%I AS source_row) AS rows',
      target_schema,
      target_table,
      target_table
    );
  END LOOP;
END
$proof$;

DO $proof$
BEGIN
  IF to_regclass(format('%I.fact_records', current_setting('eshu.proof_schema'))) IS NOT NULL THEN
    RAISE EXCEPTION 'proof schema must not shadow retained fact_records';
  END IF;
END
$proof$;
COMMIT;
