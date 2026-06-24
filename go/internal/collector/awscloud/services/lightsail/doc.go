// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package lightsail maps Amazon Lightsail instance, managed relational
// database, load balancer, block-storage disk, and static IP metadata into AWS
// cloud collector facts.
//
// The scanner emits reported-confidence resources for instances, databases,
// load balancers, disks, and static IPs plus the Lightsail-internal
// relationships for load-balancer-to-instance, instance-to-disk, and
// instance-to-static-IP evidence. Every node resource_id and every
// relationship join key is the bare Lightsail resource name, so the internal
// edges resolve the nodes this scanner publishes. Resource ARNs come from the
// Lightsail API and are used directly; the scanner never synthesizes an ARN or
// hardcodes a partition. Instance access details, default key-pair private
// keys, database master passwords, certificate bodies, and every Lightsail
// mutation API stay outside this package contract.
package lightsail
