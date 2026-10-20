// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package vpclattice maps Amazon VPC Lattice service network, service, target
// group, and listener metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for service networks,
// services, target groups, and listeners plus relationships for
// service-network-to-VPC and service-network-to-service associations,
// listener-in-service membership, target-group-to-VPC placement,
// target-group-to-service use, target-group-to-target registration (Lambda
// function, EC2 instance, and Application Load Balancer), and the
// service-to-ACM-certificate binding. Auth-policy bodies, resource-policy
// bodies, and any data-plane payload stay outside this package contract: the
// scanner is metadata-only and never mutates VPC Lattice state.
package vpclattice
