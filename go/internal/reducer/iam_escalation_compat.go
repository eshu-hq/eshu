// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/reducer/iamescalation"

// This file is the reducer root's compatibility surface for the IAM
// privilege-escalation edge family, which moved to [iamescalation]
// (issue #6061). It carries only the two names that still have a caller
// outside the family: the writer type DefaultHandlers declares and cmd/reducer
// satisfies, and the failure-class literal internal/storage/postgres' readiness
// claim gate matches. Root-internal call sites (the additive-domain registry)
// name [iamescalation] directly instead of going through a forwarder.

// IAMEscalationEdgeWriter is the root spelling of
// [iamescalation.IAMEscalationEdgeWriter].
type IAMEscalationEdgeWriter = iamescalation.IAMEscalationEdgeWriter

// IAMEscalationNodesNotReadyFailureClass is the root spelling of
// [iamescalation.IAMEscalationNodesNotReadyFailureClass]. internal/storage/
// postgres matches this exact string when it decides to re-enqueue rather than
// dead-letter, so the literal is a storage contract, not just a Go identifier.
const IAMEscalationNodesNotReadyFailureClass = iamescalation.IAMEscalationNodesNotReadyFailureClass
