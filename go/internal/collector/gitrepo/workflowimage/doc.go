// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package workflowimage emits container image evidence facts from GitHub
// Actions workflow files.
//
// It records which images a workflow references and, where the reference
// resolves, the digest behind it — the CI half of the image lineage that
// runtime collectors complete.
package workflowimage
