package reducer

// eshuSearchDocumentRetireQuery removes search-document facts in one generation
// that are not in the freshly written set, so a source row dropped within a
// generation retires its document. An empty written set matches the empty array
// and retires every document for the generation.
const eshuSearchDocumentRetireQuery = `
DELETE FROM fact_records
WHERE fact_kind = $1
  AND scope_id = $2
  AND generation_id = $3
  AND fact_id <> ALL($4::text[])
`

const eshuSearchIndexRetireTermsQuery = `
DELETE FROM eshu_search_index_terms
WHERE scope_id = $1
  AND generation_id = $2
  AND document_id <> ALL($3::text[])
`

const eshuSearchIndexRetireDocumentsQuery = `
DELETE FROM eshu_search_index_documents
WHERE scope_id = $1
  AND generation_id = $2
  AND document_id <> ALL($3::text[])
`

// eshuSearchDocumentBatchFactInsertQuery is a search-document-local bulk insert
// using unnest so all N fact rows are sent in a single round-trip. It is
// intentionally separate from canonicalReducerFactInsertQuery (which is a
// single-row parameterised statement shared by many writers) so we do not
// disturb those callers.
const eshuSearchDocumentBatchFactInsertQuery = `
INSERT INTO fact_records (
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    collector_kind,
    source_confidence,
    source_system,
    source_fact_key,
    source_uri,
    source_record_id,
    observed_at,
    ingested_at,
    is_tombstone,
    payload
)
SELECT
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    collector_kind,
    source_confidence,
    source_system,
    source_fact_key,
    source_uri,
    source_record_id,
    observed_at,
    ingested_at,
    is_tombstone,
    payload::jsonb
FROM unnest(
    $1::text[],
    $2::text[],
    $3::text[],
    $4::text[],
    $5::text[],
    $6::text[],
    $7::text[],
    $8::text[],
    $9::text[],
    $10::text[],
    $11::text[],
    $12::timestamptz[],
    $13::timestamptz[],
    $14::bool[],
    $15::text[]
) AS t(
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    collector_kind,
    source_confidence,
    source_system,
    source_fact_key,
    source_uri,
    source_record_id,
    observed_at,
    ingested_at,
    is_tombstone,
    payload
)
ON CONFLICT (fact_id) DO UPDATE SET
    fact_kind         = EXCLUDED.fact_kind,
    stable_fact_key   = EXCLUDED.stable_fact_key,
    collector_kind    = EXCLUDED.collector_kind,
    source_confidence = EXCLUDED.source_confidence,
    source_system     = EXCLUDED.source_system,
    source_fact_key   = EXCLUDED.source_fact_key,
    source_uri        = EXCLUDED.source_uri,
    source_record_id  = EXCLUDED.source_record_id,
    observed_at       = EXCLUDED.observed_at,
    ingested_at       = EXCLUDED.ingested_at,
    is_tombstone      = EXCLUDED.is_tombstone,
    payload           = EXCLUDED.payload
`

// eshuSearchIndexBatchDocumentUpsertQuery bulk-upserts all index-document rows
// for a scope in one round-trip using unnest.
const eshuSearchIndexBatchDocumentUpsertQuery = `
INSERT INTO eshu_search_index_documents (
    scope_id,
    generation_id,
    document_id,
    fact_id,
    repo_id,
    source_kind,
    document,
    document_length,
    updated_at
)
SELECT scope_id, generation_id, document_id, fact_id, repo_id, source_kind,
       document::jsonb, document_length, updated_at
FROM unnest(
    $1::text[],
    $2::text[],
    $3::text[],
    $4::text[],
    $5::text[],
    $6::text[],
    $7::text[],
    $8::int[],
    $9::timestamptz[]
) AS t(scope_id, generation_id, document_id, fact_id, repo_id, source_kind,
       document, document_length, updated_at)
ON CONFLICT (scope_id, generation_id, document_id) DO UPDATE SET
    fact_id         = EXCLUDED.fact_id,
    repo_id         = EXCLUDED.repo_id,
    source_kind     = EXCLUDED.source_kind,
    document        = EXCLUDED.document,
    document_length = EXCLUDED.document_length,
    updated_at      = EXCLUDED.updated_at
`

// eshuSearchIndexRefreshDocumentTermsQuery removes all current terms for the
// listed document IDs so the subsequent bulk insert replaces them atomically.
// This replaces the N per-document DELETE statements with a single ANY-array call.
const eshuSearchIndexRefreshDocumentTermsQuery = `
DELETE FROM eshu_search_index_terms
WHERE scope_id      = $1
  AND generation_id = $2
  AND document_id   = ANY($3::text[])
`

// eshuSearchIndexBatchTermUpsertQuery bulk-upserts all term rows for a scope in
// one round-trip using unnest. Each element in the parallel arrays corresponds to
// one (document_id, term) pair; the document_id array is repeated per term.
// Arg layout: $1=scope_id, $2=generation_id, $3=document_ids, $4=terms,
// $5=term_keys, $6=term_frequencies.
const eshuSearchIndexBatchTermUpsertQuery = `
INSERT INTO eshu_search_index_terms (
    scope_id,
    generation_id,
    document_id,
    term_key,
    term,
    term_frequency
)
SELECT $1, $2, document_id, term_key, term, term_frequency
FROM unnest($3::text[], $4::text[], $5::text[], $6::int[])
     AS t(document_id, term, term_key, term_frequency)
ON CONFLICT (scope_id, generation_id, term_key, document_id) DO UPDATE SET
    term           = EXCLUDED.term,
    term_frequency = EXCLUDED.term_frequency
`

const eshuSearchIndexStatsUpsertQuery = `
INSERT INTO eshu_search_index_stats (
    scope_id,
    generation_id,
    document_count,
    average_document_length,
    updated_at
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (scope_id, generation_id) DO UPDATE SET
    document_count          = EXCLUDED.document_count,
    average_document_length = EXCLUDED.average_document_length,
    updated_at              = EXCLUDED.updated_at
`
