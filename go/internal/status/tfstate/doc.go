// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package tfstate holds the Terraform-state family of the status report:
// per-locator observed state serials and the recent warning-fact evidence
// collected against them. The root internal/status package aggregates this
// package into RawSnapshot and Report, so the root imports tfstate; this
// package must never import the root or a sibling leaf.
//
// LocatorSerial reports the most recent observed state serial for one
// Terraform-state scope, keyed by the scope-level safe locator hash so the
// report never carries raw bucket names, S3 keys, or local file paths.
// LocatorWarning reports recent warning_fact observations for one
// Terraform-state scope, grouped by warning_kind so operators can spot
// patterns without scanning the full fact stream. WarningSummary reports
// bounded warning totals by warning kind, reason, and scope class for
// release-gate readback.
//
// CloneSerials and CloneWarnings return defensive copies; CloneWarnings also
// backfills Severity/Actionability from tfstatewarning.Classify when a row
// arrives without them, so a caller reading a row's classification never has
// to fall back to the raw warning_kind/reason pair itself.
// SortSerials and SortWarnings order rows deterministically (by safe locator
// hash, then warning_kind, then ObservedAt descending) so JSON output is
// stable across reads. GroupWarningsByKind buckets warnings per locator and
// kind; SummarizeWarnings folds warnings into deterministic aggregate counts.
// MaxRecentWarnings caps the number of recent warning rows the admin status
// surface returns per safe_locator_hash — Postgres owns the canonical
// history, this bound only prevents the JSON projection from growing
// without limit across restarts.
//
// Report projects the per-locator serial and recent-warning evidence into
// the stable shape the admin status surface renders; the Postgres query
// bounds raw inputs, this projection only sorts and groups them.
//
// ReportJSON (and the row-level Serial/Warning/WarningSummary JSON
// projections) carry the operator-facing wire contract: their struct tags
// and formatting are part of the published status JSON consumed by the
// status HTTP surfaces and MCP status tools, and are locked by byte-for-byte
// goldens at internal/status/testdata/render_json_golden.json,
// render_text_golden.txt, and render_json_key_paths_golden.txt. A golden
// failure is an API break to justify, not a golden to regenerate
// reflexively.
package tfstate
