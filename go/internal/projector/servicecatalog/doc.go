// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package servicecatalog builds the service-catalog-correlation reducer
// intent from one immutable scope generation: when the generation carries at
// least one fact whose kind the central service-catalog schema registry
// recognizes (entity, ownership, repository link, dependency, API link,
// operational link, scorecard definition or result, warning), it asks the
// reducer to correlate the generation's catalog facts against repository and
// deployment truth once. The anchor is the earliest recognized catalog fact in
// original input order across every catalog kind — there is no per-kind
// priority — so the reducer claim is stable across reprojections of the same
// generation. Only the fact kind is read; no payload is decoded, and
// schema-version admission stays with root projection, which rejects an
// unsupported service-catalog schema version before any builder runs. The
// source-system label falls back from the fact's SourceRef to its
// CollectorKind to the ingestion scope's own SourceSystem, a third tier the
// shared intent helper does not have, which is why this builder takes the
// scope value and keeps its own helper. Catalog facts stay provenance-only
// here: the reducer's service_catalog_correlation handler owns every
// correlation decision and write. Root projector assembly owns lookup
// construction and lifetime, invocation order, queue writes, retries, and
// telemetry.
package servicecatalog
