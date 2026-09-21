// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind self-registers the Amazon Keyspaces (for Apache Cassandra)
// scanner with the AWS collector runtime registry through an init side effect.
// It is imported for effect only by the collector bindings aggregator.
package runtimebind
