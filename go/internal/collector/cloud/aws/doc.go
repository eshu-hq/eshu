// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awscloud defines AWS cloud scanner fact identity and runtime-neutral
// observation contracts.
//
// The package owns AWS service-kind constants, shared claim boundaries,
// reported-confidence fact envelope builders, scalar redaction helpers, and
// bounded scan-status accounting types. It does not call AWS APIs, schedule
// workflow claims, choose credentials, write graph truth, or answer queries.
//
// Service-specific scanners convert AWS API data into these contracts before
// the shared collector and reducer paths persist and materialize them. Those
// scanners must keep metadata-only services out of data-plane reads, secret
// values, policy persistence, payload capture, query result rows, named-query
// SQL bodies, prepared-statement query bodies, query history strings, and
// finding-body persistence, and mutation APIs. ECS and Lambda environment
// values plus Organizations account email/name values must be redacted through
// this package before callers build envelopes. Glue scans are limited to Data
// Catalog database, table, crawler, job, trigger, workflow, and connection
// metadata plus ARN-addressable IAM-role and S3-location relationships; job
// script bodies, default-argument values, connection passwords, JDBC credential
// URLs, connection property values, table column statistics with sample values,
// classifier custom patterns, and workflow graph payloads stay outside the
// contract. GuardDuty finding-body capture, filter criteria, threat intel set
// list contents, and IP set list contents also stay outside the contract.
package awscloud
