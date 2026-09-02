// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cloudjoin owns the in-memory identity join from an AWS endpoint
// identity to the uid of a materialized CloudResource node.
//
// [BuildCloudResourceJoinIndex] folds one scope generation's aws_resource
// facts into a [CloudResourceJoinIndex] keyed four ways — by ARN, by uid, by
// bare resource id, and by correlation anchor — so edge projection resolves an
// endpoint in O(1) with no per-edge graph round trip.
// [CloudResourceUID] computes the node identity itself, from the same inputs
// the aws_resource fact's stable key uses, so a relationship fact's resolved
// target recomputes the identical uid.
//
// The index never fabricates a uid. Every entry comes from an aws_resource
// fact that carried its own account_id and region, so a cross-account or
// cross-region ARN resolves only when that account+region resource was scanned
// in the same scope. That is the trust boundary, and it is the reason
// resolution is index membership rather than string construction.
//
// # Why this is a shared leaf
//
// The AWS relationship and security-group slices at the reducer root and the
// [iamcan] family both resolve endpoints against this same index, and a family
// package may never import the reducer root. The index therefore lives below
// both. The root keeps cloudResourceJoinIndex, buildCloudResourceJoinIndex and
// cloudResourceUID as aliases and forwarders in
// cloud_resource_join_index_compat.go so its own callers compile unchanged.
//
// Imports point strictly downward: this package reaches [factdecode],
// [payloadcore], [schemadecode] and internal/facts, and never the parent
// internal/reducer package.
//
// # Observability
//
// This package registers no instrument. A fact whose identity payload cannot
// be decoded is returned as a quarantine value to the calling handler, which
// is what increments eshu_dp_reducer_input_invalid_facts_total; a decode
// failure here is per-fact, so one malformed resource never empties the
// index for the whole scope.
package cloudjoin
