// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 DataZone client into the
// metadata-only DataZone scanner interface.
//
// The adapter uses ListDomains, GetDomain, ListProjects, ListEnvironments,
// ListDataSources, GetDataSource, and ListTagsForResource to read DataZone
// governance control-plane metadata and resource tags. It intentionally
// excludes GetAsset, GetGlossary, GetGlossaryTerm, GetListing, GetSubscription,
// the time-series and lineage reads, and every Create/Update/Delete mutation
// API, so the adapter cannot read catalog asset or glossary content or write
// DataZone state. From GetDataSource it copies only the backing-store names
// (Glue database, provisioned Redshift cluster) needed to join scanned
// resources, never the relational filter expressions or access credentials.
package awssdk
