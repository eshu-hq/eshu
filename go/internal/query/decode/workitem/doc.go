// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package workitem owns the shared work-item decode substrate for the query
// layer: the nine work_item.* typed decode wrappers, the one-argument decode
// input, the version-normalizing schema envelope, the nil-safe *bool deref,
// and the evidence-drop log helper.
//
// It exists because two read paths decode work-item facts — the work-item
// evidence family (internal/query/workitem) and the incident store
// (internal/query/incident/store) — and neither could import the other's
// unexported decoders, so the store carried verbatim forks. This leaf,
// created for #6623, is the single home both import; the forks are gone.
//
// The wrappers call the contracts-module factschema.Decode* seam and return a
// *decode.Error on a classified decode failure, so callers route a bad row to
// an input_invalid-class read-model outcome (a dropped row) instead of
// defaulting every field to "". LogEvidenceDecodeDrop exposes the drop-log
// helper, but this package never logs on its own: each read path decides to
// call it when it drops a row.
//
// Every importer spells the import the same way:
//
//	workitemdecode "github.com/eshu-hq/eshu/go/internal/query/decode/workitem"
//
// The payload-usage manifest gate recognizes qualified decode calls only
// through that qualifier (see KnownDecodeQualifiers); any other spelling
// silently drops the call sites from the manifest.
package workitem
