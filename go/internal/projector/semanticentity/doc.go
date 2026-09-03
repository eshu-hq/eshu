// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package semanticentity builds the semantic_entity_materialization reducer
// intent from a single content_entity fact. Unlike the scope-generation
// families under internal/projector, this builder is called once per input
// fact from root's per-fact projection loop, so it takes a facts.Envelope
// rather than a projectorintent.FactLookup and may return an intent for many
// facts in one generation; all of them share the repo:<repo_id> entity key,
// and root's deterministic sort plus the reducer's per-key claim collapse
// them into one unit of work. Admission is entity type first (Annotation,
// Typedef, TypeAlias, Component, Module, ImplBlock, Protocol,
// ProtocolImplementation), then per-language predicates that admit callables
// and language-specific shapes carrying real metadata, so a plain Go func or
// a bare ES module produces no intent. A fact with a blank repo_id is
// rejected because the entity key is the repository acceptance unit. The
// intent's source-system label is the raw SourceRef.SourceSystem, not the
// two-tier projectorintent.SourceSystem fallback the scope-generation
// families use -- that is the preserved pre-extraction behavior. The
// reducer's DomainSemanticEntityMaterialization handler owns the entity rows
// it writes and re-applies its own language predicates; root projector
// assembly owns the per-fact loop, intent ordering, queue writes, retries,
// and telemetry.
package semanticentity
