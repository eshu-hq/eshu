// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ownership decides, for a scoped caller, which nodes of a bounded
// impact traversal page the caller's repository grant owns (#5167). It backs
// the trace-resource-to-code, explain-dependency-path, and trace-exposure-path
// routes, whose walks cross nodes that carry no repo_id, so the grant cannot
// be a single Cypher predicate.
//
// Every node is judged by its class (ClassOf): a Repository by id, a
// repo_id-carrying label by repo_id in Go, a WorkloadInstance additionally by
// DEPLOYMENT_SOURCE to a granted Repository, a CloudResource by USES from a
// granted WorkloadInstance, and a TerraformStateResource by MATCHES_STATE from
// a granted TerraformResource. Every other class is ungranted: there is no
// third state. Checker.Check sends only the page's deduplicated keys of the
// statement-checked classes to the graph, in ChunkSize chunks, as grant-free
// owner projections whose owner ids are checked against the grant in Go, so
// statement cost does not depend on the grant size. At most MaxCheckedKeys
// keys are checked per request; keys past the cap are unchecked and therefore
// ungranted, and the verdict reports Capped so the caller reports truncated.
// An empty grant makes no graph call.
//
// FilterPaths and FilterRows drop a path whole when any node on it is not
// admitted; ResolveAnchor anchors a scoped caller on the first candidate
// node its grant owns and turns an identifier with no owned candidate into
// the same nil an unknown anchor resolves to. Withheld paths and ownership statement latency
// are exported as eshu_dp_query_impact_scoped_paths_withheld_total and
// eshu_dp_query_impact_ownership_check_duration_seconds.
package ownership
