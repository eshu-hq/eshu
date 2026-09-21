// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package timestream maps Amazon Timestream for LiveAnalytics database and
// table metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for Timestream databases and
// tables plus relationships for table-in-database, database-to-KMS-key, and
// table-to-S3 (the magnetic-store rejected-data report location) evidence.
// Time-series records, measure values, query results, and any mutation or
// record-write API stay outside this package contract: the scanner is
// metadata-only.
package timestream
