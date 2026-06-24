// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const incidentWorkItemFactRecordReadIndexesSQL = `CREATE INDEX IF NOT EXISTS fact_records_work_item_external_link_url_idx ON fact_records ((payload->>'url'), (payload->>'work_item_key'), fact_id ASC) WHERE fact_kind = 'work_item.external_link' AND is_tombstone = FALSE;
CREATE INDEX IF NOT EXISTS fact_records_work_item_record_key_idx ON fact_records ((payload->>'work_item_key'), fact_id ASC) WHERE fact_kind = 'work_item.record' AND is_tombstone = FALSE;
`
