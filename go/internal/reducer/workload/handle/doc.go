// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package handle resolves provider-asserted workload selectors
// through the #5385 handle ladder for reducer consumers.
//
// A provider-asserted workload reference is either the full scoped node id
// (workload:<repo_id>:<name>) or the public handle (workload:<name>). The
// ladder resolves the full id by exact match and the handle by bare name
// within a caller-supplied candidate set, failing closed on ambiguity: a
// handle naming workloads in more than one repository resolves to none of
// them. See docs/internal/design/5385-workload-identity-compatibility.md
// sections 1.4 and 2.3.
package handle
