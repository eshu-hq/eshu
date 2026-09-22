// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// This file is the status root's compatibility surface for the generation family
// that moved to [generation] (issue #6775). It carries no behavior change: every
// alias names the same type and every constant the same value, so the packages
// importing internal/status keep compiling unchanged. Each entry is deleted
// once its last caller has moved to the leaf; see the importer-migration child
// issue. A later generation move adds a stanza here and never creates a second
// compat file for this family.

import "github.com/eshu-hq/eshu/go/internal/status/generation"

// Generation lifecycle and transition sections.
//
// Deprecated: use the [generation] names.
type (
	GenerationTransitionSnapshot = generation.TransitionSnapshot
	GenerationLifecycleFilter    = generation.LifecycleFilter
	GenerationLifecycleRecord    = generation.LifecycleRecord
	GenerationLifecyclePage      = generation.LifecyclePage
	GenerationQueueStatus        = generation.QueueStatus
	GenerationLatestFailure      = generation.LatestFailure
)

// Generation lifecycle page bounds.
//
// Deprecated: use the [generation] constants.
const (
	MaxGenerationLifecycleLimit     = generation.MaxLifecycleLimit
	DefaultGenerationLifecycleLimit = generation.DefaultLifecycleLimit
)
