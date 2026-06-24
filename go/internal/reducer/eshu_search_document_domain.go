// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// DomainEshuSearchDocument is the reducer domain that projects curated
// EshuSearchDocument records into the Postgres search-lane read model. It is the
// design-430 curated search projection, kept separate from canonical graph
// writes. Runtime registration and intent emission for this domain are wired
// after the design-430 benchmark gate (#2235) selects the search-lane backing.
const DomainEshuSearchDocument Domain = "eshu_search_document"

// EshuSearchDocumentFactKind is the durable fact kind for one curated search
// document persisted in fact_records. Readers join on the active generation, so
// documents from superseded generations are retired automatically.
const EshuSearchDocumentFactKind = "reducer_eshu_search_document"
