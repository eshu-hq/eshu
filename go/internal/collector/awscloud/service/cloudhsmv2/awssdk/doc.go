// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 CloudHSM v2 client into the
// metadata-only CloudHSM v2 scanner interface.
//
// The adapter uses DescribeClusters and DescribeBackups to read CloudHSM v2
// cluster and backup control-plane metadata; both reads return the resource tag
// list inline, so no separate tag call is needed. It intentionally excludes
// every CreateCluster/CreateHsm/DeleteCluster/DeleteHsm/DeleteBackup/
// RestoreBackup/CopyBackupToRegion/ModifyCluster/ModifyBackupAttributes/
// InitializeCluster mutation and the resource-policy reads, so the adapter
// cannot mutate CloudHSM state, initialize a cluster (the flow that exposes the
// Pre-Crypto Officer password), or read key material. Certificate and CSR
// bodies are inspected only to test presence and are never copied out.
package awssdk
