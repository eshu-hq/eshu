// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package detect holds the hardcoded-secret detector that migration 138
// derives content_file_secret_lines from, as Go values both the query layer
// and the collector redactor bind to.
//
// SQLPattern is the Postgres pattern; Kinds, Classify, and ArmSources describe
// the finding-kind CASE in order; Spans finds the credential values in a line.
// The package has no dependencies so it can sit under the query layer and the
// collector without an import cycle. Changing any value needs a new migration
// that replaces eshu_secret_line_findings (migration 138 is never edited); the
// binding test in the query package fails on a Go-only edit.
package detect
