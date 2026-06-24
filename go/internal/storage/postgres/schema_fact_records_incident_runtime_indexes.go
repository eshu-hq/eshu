// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const incidentRuntimeFactRecordReadIndexesSQL = `CREATE INDEX IF NOT EXISTS fact_records_service_catalog_operational_link_url_idx ON fact_records ((payload->>'url'), (payload->>'provider'), (payload->>'entity_ref'), fact_id ASC) WHERE fact_kind = 'service_catalog.operational_link' AND is_tombstone = FALSE;
CREATE INDEX IF NOT EXISTS fact_records_kubernetes_correlation_image_lookup_idx ON fact_records ((payload->>'source_digest'), (payload->>'image_ref'), (payload->>'outcome'), fact_id ASC) WHERE fact_kind = 'reducer_kubernetes_correlation' AND is_tombstone = FALSE;
`
