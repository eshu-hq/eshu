// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/kubernetescorrelation"
)

// This file is the transitional compatibility surface for the kubernetes
// correlation family that moved to [kubernetescorrelation] (issue #6061). It
// carries only the name that still has a caller outside the reducer module
// boundary: internal/storage/postgres' reducer queue readiness SQL, which
// matches this exact string when it decides to re-enqueue rather than
// dead-letter. Everything else the family exports is reached as
// kubernetescorrelation.X, and this entry is deleted once its last caller has
// moved.

// KubernetesCorrelationNodesNotReadyFailureClass is the retryable failure
// class the #388 node-slice readiness gate returns. internal/storage/postgres'
// reducer queue matches this exact string when it decides to re-enqueue rather
// than dead-letter, so the literal is a storage contract, not just a Go
// identifier. See
// [kubernetescorrelation.KubernetesCorrelationNodesNotReadyFailureClass].
const KubernetesCorrelationNodesNotReadyFailureClass = kubernetescorrelation.KubernetesCorrelationNodesNotReadyFailureClass
