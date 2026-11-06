// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package main is the SBOM shadow extractor for the #8877 stdio window.
//
// The binary speaks the collector SDK satellite protocol over stdio: it reads
// one extensionhost.Request from stdin and writes one SDK result to stdout.
// Every invocation parses the single pinned CycloneDX document baked into
// the binary (testdata/cyclonedx_image_subject.json, a byte-identical copy
// of the lodash fixture behind the document package's checked-in assertions)
// through the existing sbom document parser and returns the parsed envelopes
// as SDK facts with state complete.
//
// The result state is complete, not unchanged: the SDK host validator
// rejects an unchanged result that carries facts
// (validateStateFactAllowance), and this leaf's contract is real extraction
// counts, so the facts must ride along. The status row reports the real
// parsed fact count. v1 performs no source observation, holds no
// credentials, and makes no network calls; the live-read half is a later
// leaf. The host validates the echoed claim field-for-field, so the emitter
// returns the incoming claim verbatim and derives the parser boundary
// (scope, generation, instance, fencing token) from it.
package main
