// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ecs maps Amazon ECS observations into AWS cloud fact envelopes.
//
// The package owns scanner-level ECS fact selection for clusters, services,
// task definitions, tasks, relationships, and running-task container image
// references. AWS SDK pagination, credentials, persistence, graph projection,
// and reducer-owned correlation live outside this package.
package ecs
