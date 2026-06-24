// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 DocumentDB Elastic Clusters
// client into the metadata-only DocumentDB Elastic scanner interface.
//
// The adapter uses ListClusters, GetCluster, and ListTagsForResource to read
// elastic cluster control-plane metadata and resource tags. ListClusters
// returns identity-only summaries, so GetCluster is called per cluster for the
// full control-plane metadata. The adapter intentionally excludes every
// Create/Update/Delete/Copy/Restore/Apply mutation API and every document,
// collection, index, snapshot, and query read, so it cannot read database
// contents, read the admin password, or write DocumentDB Elastic state.
package awssdk
