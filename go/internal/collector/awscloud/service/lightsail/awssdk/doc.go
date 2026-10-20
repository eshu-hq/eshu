// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Lightsail client into the
// metadata-only Lightsail scanner interface.
//
// The adapter uses GetInstances, GetRelationalDatabases, GetLoadBalancers,
// GetDisks, and GetStaticIps. It intentionally excludes every Lightsail
// mutation and sensitive-read API, including CreateInstances, DeleteInstance,
// RebootInstance, StartInstance, StopInstance, CreateDiskSnapshot,
// AttachDisk, DetachDisk, AttachStaticIp, DetachStaticIp,
// GetInstanceAccessDetails, DownloadDefaultKeyPair, and
// GetRelationalDatabaseMasterUserPassword, so a mutation or secret read can
// never be reached through the scanner. A reflective guard test enforces the
// exclusion on the adapter-local apiClient interface.
package awssdk
