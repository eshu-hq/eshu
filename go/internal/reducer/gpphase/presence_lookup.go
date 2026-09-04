// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gpphase

import "context"

// EndpointPresenceLookup answers the uid-exact cross-scope readiness question
// (issue #1380). MissingUIDs returns the subset of uids that have no presence
// row for the keyspace, computed with ONE bounded query
// (WHERE keyspace=$1 AND uid = ANY($2)) and an in-memory set difference — never
// an N+1 per-uid probe, which the performance contract forbids. An empty input
// yields an empty result and no query.
//
// This is the read half of the endpoint-presence primitive, and it lives here
// for the same reason [ReadinessLookup] and [ReadinessPrefetch] do: a domain
// family gates its graph writes on it, and a family may not import the reducer
// root. The write half — [EndpointPresenceRow], [EndpointPresenceWriter], and
// [PublishEndpointPresence] — moved here too (issue #6061, see
// endpoint_presence.go): they were the last root-owned pieces blocking the
// ec2, s3, iam, and security_group families from splitting out without
// importing the root. The node materializers that call
// PublishEndpointPresence (CloudResource, KubernetesWorkload) still live at
// the root; only the primitive moved.
//
// A presence row proves that one specific node uid is committed in the
// canonical graph. That is a strictly different question from the
// same-scope/same-generation milestone a [PhaseKey] names, and neither answers
// the other: a keyspace can have reached a phase for its own scope while the
// uid a cross-scope join needs is still absent.
type EndpointPresenceLookup interface {
	MissingUIDs(ctx context.Context, keyspace Keyspace, uids []string) ([]string, error)
}
