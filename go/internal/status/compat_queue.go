// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// This file is the status root's compatibility surface for the queue family
// that moved to [queue] (issue #6775). It carries no behavior change: every
// alias names the same type and every constant the same value, so the packages
// importing internal/status keep compiling unchanged. Each entry is deleted
// once its last caller has moved to the leaf; see the importer-migration child
// issue. A later queue move adds a stanza here and never creates a second
// compat file for this family.

import "github.com/eshu-hq/eshu/go/internal/status/queue"

// Queue blockage and failure sections.
//
// Deprecated: use the [queue] names.
type (
	QueueBlockage        = queue.Blockage
	QueueFailureSnapshot = queue.FailureSnapshot
)
