// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 Cloud Map (Service Discovery) calls
// into scanner-owned metadata.
//
// The adapter is read-only. It pages ListNamespaces, fans out to a
// NAMESPACE_ID-filtered ListServices per namespace, and reads tags with
// ListTagsForResource. The internal apiClient interface deliberately excludes
// every Cloud Map mutation API (Create/Update/Delete for namespaces and
// services, RegisterInstance, DeregisterInstance,
// UpdateInstanceCustomHealthStatus, TagResource, UntagResource) and every
// instance discovery/read API (DiscoverInstances, DiscoverInstancesRevision,
// GetInstance, ListInstances, GetInstancesHealthStatus). A reflection-based
// test verifies the exclusion so a future SDK refactor cannot quietly add one
// back.
//
// The adapter records the instance count from the Cloud Map service summary and
// never reads, lists, or discovers instances. Instance attribute maps, which can
// hold caller-defined secrets, never enter the scanner.
package awssdk
