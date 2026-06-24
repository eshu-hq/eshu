// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package networkmanager maps AWS Network Manager global-network and
// core-network metadata into AWS cloud collector facts.
//
// AWS Network Manager is a global service: its control plane is reachable only
// in one region per partition, so the awssdk adapter pins that region while the
// scan boundary keeps its claimed account and region for attribution. The
// scanner emits reported-confidence resources for global networks, sites,
// devices, links, connections, and core networks, plus relationships for
// child-in-global-network membership, device/link placement at a site, the
// device-to-link association, the connection-to-device references, and each
// transit gateway registration. The transit gateway edge is keyed by the bare
// transit gateway id (the resource_id the transit gateway scanner publishes),
// extracted from the registration's transit gateway ARN. All synthesized ARNs
// are partition-aware so GovCloud and China edges join real nodes. The scanner
// reads control-plane metadata only and never mutates Network Manager state.
package networkmanager
