// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package clientgo is the client-go adapter for the Kubernetes live collector.
// It is the only place client-go and the Kubernetes typed API are imported, so
// the collector source stays backend-neutral and unit-testable with fakes.
//
// The adapter authenticates read-only through either the in-cluster service
// account (AuthModeInCluster) or a kubeconfig file (AuthModeKubeconfig), builds
// a typed clientset, and implements kuberneteslive.Client by listing a fixed
// core resource set with bounded pagination.
//
// The adapter is read-only and metadata-only by construction: it maps typed
// objects into the collector's neutral metadata views, including IRSA and GKE
// Workload Identity annotation targets that the source later fingerprints or
// digests before fact emission. It never reads Secret values, ConfigMap data
// payloads, environment variable values, or container logs, and it never issues
// a write, patch, delete, exec, attach, portforward, or log request. A forbidden
// list degrades to a partial result with a warning reason rather than aborting
// the snapshot.
package clientgo
