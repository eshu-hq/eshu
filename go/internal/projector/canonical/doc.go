// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package canonical turns one scope generation's facts into the canonical
// materialization the graph writers persist: the repository, directory, file,
// entity, import, module, parameter, class-member and nested-function rows of
// the source-local code graph, plus the typed OCI-registry, package-registry
// and Terraform-state row families.
//
// [BuildMaterialization] is the entry point. It reads only the facts it is
// given, never the store, so the same generation always produces the same
// materialization — the determinism the Ifá replay gate pins. Extraction never
// swallows a malformed fact: a fact whose payload is missing a required field
// is returned as a [decode.QuarantinedFact] for the caller to dead-letter,
// while a present-but-empty identity field is a valid decode that the row
// builders' own identity gate drops.
//
// The package decodes through [decode] and holds no queue, retry, telemetry or
// transaction concern; the projector runtime owns those and owns the order the
// extractors run in. It writes nothing itself — every row it produces is a
// value the caller hands to a writer.
package canonical
