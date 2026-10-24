// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package detect holds the hardcoded-secret detector that
// content_file_secret_lines is derived from (the eshu_secret_line_findings
// function of migration 234, #8833, over the table and triggers of migration
// 138), as Go values both the query layer and the content-store redactors bind
// to.
//
// SQLPattern and MarkerPrefix are the line gate; Kinds, Classify, and
// ArmSources describe the finding-kind CASE in order; Finding is the stored
// finding of one line; Spans finds the credential values in a line. The
// package has no dependencies so it can sit under the query layer and the
// collector without an import cycle. Changing any value needs a new migration
// that replaces eshu_secret_line_findings (shipped migrations are never
// edited); the binding test in the query package fails on a Go-only edit.
package detect
