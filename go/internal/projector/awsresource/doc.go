// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awsresource builds the AWS resource-materialization reducer intent
// for one scope generation.
//
// BuildAWSResourceMaterializationReducerIntent returns a single scope-keyed
// intent when the generation carries any aws_resource fact, anchored to the
// earliest such fact in original input order so the reducer claim is stable
// across reprojections of the same generation. The package makes no admission
// or correlation decision: the reducer owns the CloudResource node write.
//
// The aws_resource_materialization:<scope> entity key it emits is shared with
// the other AWS families that gate readiness on the CloudResource substrate,
// and internal/storage/postgres derives the cloud-resource-node queue conflict
// key from that prefix only for a domain whose resource-conflict policy is
// marked safe (today this domain alone); the sibling families sharing the key
// group by resource_scope or the default. Callers must not treat the key as
// private to this package.
//
// The package consumes the neutral internal/projector/intent contract and must
// not import the root projector package; root imports it to dispatch.
package awsresource
