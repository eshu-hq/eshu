// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// SpanQueryCodeTopicPartition measures one SQL probe partition of a code-topic investigation.
const SpanQueryCodeTopicPartition = "query.code_topic_partition"

// SpanQueryHardcodedSecretInvestigation wraps the prompt-facing hardcoded
// secret investigation route.
const SpanQueryHardcodedSecretInvestigation = "query.hardcoded_secret_investigation"

// SpanQueryImportDependencyInvestigation wraps the prompt-facing import and
// module-dependency investigation route.
const SpanQueryImportDependencyInvestigation = "query.import_dependency_investigation"

// SpanQueryCallGraphMetrics wraps the prompt-facing call-graph metrics route.
const SpanQueryCallGraphMetrics = "query.call_graph_metrics"

// SpanQueryGraphSummaryPacket wraps the bounded graph summary packet route
// (hot entities, key relationships, ecosystem map).
const SpanQueryGraphSummaryPacket = "query.graph_summary_packet"

// SpanQueryGraphEntityInventory wraps the graph entity facet-count and
// optional bounded-list route.
const SpanQueryGraphEntityInventory = "query.graph_entity_inventory"

// SpanQueryHybridRerank times the in-request hybrid (BM25 plus vector) re-rank
// of one find_code, search_entity_content, or search_file_content page: row
// projection, request-local index build, and ranking (#7746).
const SpanQueryHybridRerank = "query.hybrid_rerank"
