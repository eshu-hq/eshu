// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

// UpsertSQL exposes the production upsert statement to the external live
// tests, which derive a guard-stripped comparator from it instead of
// hand-copying the statement.
const UpsertSQL = upsertSQL

// ReadSQL exposes the production read statement to the external live tests.
const ReadSQL = readSQL
