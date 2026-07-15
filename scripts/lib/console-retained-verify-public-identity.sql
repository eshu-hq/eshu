SELECT set_config('eshu.proof_schema', :'proof_schema', false);
DO $proof$
DECLARE
  target_schema text := current_setting('eshu.proof_schema');
  baseline record;
  current_count bigint;
  current_digest text;
BEGIN
  FOR baseline IN EXECUTE format(
    'SELECT table_name, row_count, row_digest FROM %I._public_identity_snapshots ORDER BY table_name',
    target_schema
  ) LOOP
    EXECUTE format(
      'SELECT count(*), COALESCE(md5(string_agg(row_json, chr(10) ORDER BY row_json)), md5('''')) '
      'FROM (SELECT to_jsonb(source_row)::text AS row_json FROM public.%I AS source_row) AS rows',
      baseline.table_name
    ) INTO current_count, current_digest;
    IF current_count <> baseline.row_count OR current_digest <> baseline.row_digest THEN
      RAISE EXCEPTION 'retained public identity table % changed during isolated proof', baseline.table_name;
    END IF;
  END LOOP;
END
$proof$;
