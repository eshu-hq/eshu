// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cloudinventory resolves provider cloud-inventory raw identity into
// the shared canonical cloud_resource_uid keyspace.
//
// It is the provider-neutral identity core the reducer cloud-inventory
// admission path uses to admit aws_resource, gcp_cloud_resource, and
// azure_cloud_resource source facts. The resolver is pure and deterministic:
// the same provider plus raw identity always yields the same uid, so concurrent
// or retried reducer admissions converge on one canonical row. Identities that
// cannot be keyed safely are counted as unresolved, ambiguous, or unsupported
// and never fabricate a uid.
//
// The package owns identity resolution only. It does not query Postgres or a
// graph backend, does not classify drift, and does not project graph nodes or
// edges. Reducer wiring decides when to load, persist, or publish.
package cloudinventory
