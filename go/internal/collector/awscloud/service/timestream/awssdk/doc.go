// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Timestream-write client into the
// metadata-only Timestream scanner interface.
//
// The adapter uses ListDatabases, ListTables, and ListTagsForResource to read
// Timestream database and table control-plane metadata and resource tags. It
// intentionally excludes WriteRecords, every batch-load API, all Create/Update/
// Delete mutation APIs, and the entire timestream-query module (which it never
// imports), so the adapter cannot read time-series records or measures or write
// Timestream state.
package awssdk
