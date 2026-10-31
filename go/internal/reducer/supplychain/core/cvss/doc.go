// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cvss computes CVSS base scores and severity buckets for the
// supply-chain impact reducer (issue #9693).
//
// BaseScore parses a CVSS v3.0, v3.1, or v4.0 vector string and returns its
// base score using the FIRST scoring algorithms. DeriveSeverity combines a
// source-reported score, a vector, and a severity label into one score and
// bucket with a fixed order of precedence, so a record whose source published
// only a vector or only a label still gets a severity. BucketSQL renders the
// same thresholds and label table as the SQL CASE expression that Postgres
// uses for the stored and the read-time severity bucket, so the Go and SQL
// buckets cannot drift.
//
// The package is pure: no I/O, no logging, no clock, no shared state, and no
// imports from Eshu. It never panics on input and never invents a score: a
// malformed or unsupported vector returns an error, and a label yields a
// bucket but never a number.
package cvss
