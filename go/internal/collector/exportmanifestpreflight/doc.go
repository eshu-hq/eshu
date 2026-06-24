// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package exportmanifestpreflight classifies offline documentation export manifests.
//
// The package validates bounded JSON manifests for explicitly supplied GitHub,
// Jira, Slack, Teams, Google Workspace export, and generic documentation export
// inputs. It returns metadata-only counts and low-cardinality warning classes,
// including ACL gaps, duplicate source items, attachment references, unsafe
// paths, token-bearing URLs, and credential-looking paths. It does not read
// referenced export files, infer provider ACLs, emit facts, call providers,
// unpack archives, or enable any runtime ingestion path.
package exportmanifestpreflight
