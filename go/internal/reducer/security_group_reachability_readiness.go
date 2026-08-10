// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "fmt"

// securityGroupReachabilityNotReadyError marks a readiness-gate miss as retryable
// so the durable queue re-runs the intent once the missing canonical nodes
// commit, instead of failing terminally or writing edges against absent nodes.
type securityGroupReachabilityNotReadyError struct {
	scopeID      string
	generationID string
	keyspace     GraphProjectionKeyspace
}

func (e securityGroupReachabilityNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical nodes not committed on keyspace %s for scope %s generation %s",
		e.keyspace, e.scopeID, e.generationID,
	)
}

func (securityGroupReachabilityNotReadyError) Retryable() bool { return true }

// SecurityGroupReachabilityNodesNotReadyFailureClass identifies an in-handler readiness-gate miss: the
// security-group reachability edge intent ran before its upstream cloud-resource
// canonical-nodes-committed phase published for the same acceptance unit. The
// durable claim gate (reducerClaimReadinessRequirementsSQL) normally prevents
// that, so this fires only in the claim/handle race window where the handler's
// own ReadinessLookup disagrees with the claim-time gate.
//
// Enrolled in nonCountingReducerRetryFailureClasses (#5046) so the miss never
// erodes the retry budget and dead-letters a still-pending intent that the
// succeeded-only reopen path would never reopen. Declaring the constant is not
// what enrolls it -- see that list's doc comment, and the go/ast completeness
// test that now checks every readiness class is registered.
const SecurityGroupReachabilityNodesNotReadyFailureClass = "security_group_reachability_nodes_not_ready"

func (securityGroupReachabilityNotReadyError) FailureClass() string {
	return SecurityGroupReachabilityNodesNotReadyFailureClass
}
