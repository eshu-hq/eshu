// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Clean Rooms client into the
// metadata-only Clean Rooms scanner interface.
//
// The adapter uses ListCollaborations, ListConfiguredTables, GetConfiguredTable
// (only to resolve the backing-table reference for the Glue edge),
// ListMemberships, and ListTagsForResource to read Clean Rooms control-plane
// metadata and resource tags. It intentionally excludes every protected-query
// and job run, every result read, every analysis-rule and analysis-template
// body read, and all Create/Update/Delete mutation APIs, so the adapter cannot
// run protected queries, read query results, or write Clean Rooms state.
package awssdk
