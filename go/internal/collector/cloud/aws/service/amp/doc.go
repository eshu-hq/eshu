// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package amp maps Amazon Managed Service for Prometheus workspace, rule-groups
// namespace, and managed-collector (scraper) metadata into AWS cloud collector
// facts.
//
// The scanner emits reported-confidence resources for AMP workspaces,
// rule-groups namespaces (names only), and scrapers, plus relationships for
// workspace-to-KMS-key, namespace-in-workspace, scraper-to-EKS-cluster,
// scraper-to-workspace, scraper-to-subnet, and scraper-to-security-group
// evidence. Ingested time-series samples, query results, alert-manager
// definitions, rule-group definition bodies, scrape-configuration bodies, and
// any mutation API stay outside this package contract: the scanner is
// metadata-only.
package amp
