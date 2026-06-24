// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package docdbelastic maps Amazon DocumentDB Elastic Clusters control-plane
// metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for DocumentDB Elastic
// clusters plus relationships for cluster-to-subnet, cluster-to-security-group,
// cluster-to-KMS-key, and cluster-to-admin-secret evidence. Document contents,
// collections, indexes, query results, the cluster endpoint connection string,
// the admin user name, and the admin password stay outside this package
// contract: the scanner is metadata-only. It is a distinct service kind from
// the classic instance-based DocumentDB scanner (ServiceDocDB).
package docdbelastic
