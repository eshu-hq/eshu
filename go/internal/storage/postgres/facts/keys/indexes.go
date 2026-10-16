// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factkeys

// UnavailablePartitionIndexesSQL is one UNION arm, ending in ORDER BY 1, that
// returns the name of every key sidecar partition index the deletes depend on
// that is not a valid, ready, non-unique btree on exactly (scope_id,
// generation_id, fact_id) with no expression or predicate. It reads only
// pg_catalog and resolves names through the session's search_path, as the
// deletes do. Generation retention appends it to its key index check and
// refuses the cycle when any name comes back: without the index each delete
// skip-scans or seq-scans its table under the scope lock (a pmck generation
// delete measured 1,007 ms with 2,049 index searches per candidate).
// relationship_reference_candidate_keys_partition_idx (migration 058) is in
// the set because its delete has the same dependency.
const UnavailablePartitionIndexesSQL = `SELECT expected.index_name
FROM (VALUES
    ('relationship_reference_candidate_keys_partition_idx', 'relationship_reference_candidate_keys'),
    ('package_manifest_consumption_keys_partition_idx', 'package_manifest_consumption_keys'),
    ('package_registry_identity_keys_partition_idx', 'package_registry_identity_keys')
) AS expected(index_name, table_name)
WHERE NOT EXISTS (
    SELECT 1
    FROM pg_index AS index_state
    JOIN pg_class AS index_relation ON index_relation.oid = index_state.indexrelid
    JOIN pg_am AS access_method ON access_method.oid = index_relation.relam
    WHERE index_state.indexrelid = to_regclass(expected.index_name)
      AND index_state.indrelid = to_regclass(expected.table_name)
      AND access_method.amname = 'btree'
      AND index_state.indisvalid
      AND index_state.indisready
      AND NOT index_state.indisunique
      AND index_state.indnkeyatts = 3
      AND index_state.indnatts = 3
      AND index_state.indexprs IS NULL
      AND index_state.indpred IS NULL
      AND ARRAY(
          SELECT attribute.attname::text
          FROM unnest(index_state.indkey::int2[]) WITH ORDINALITY AS key(attnum, position)
          JOIN pg_attribute AS attribute
            ON attribute.attrelid = index_state.indrelid AND attribute.attnum = key.attnum
          ORDER BY key.position
      ) = ARRAY['scope_id', 'generation_id', 'fact_id']
)
ORDER BY 1
`
