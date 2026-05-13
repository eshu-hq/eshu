// Package ecs maps Amazon ECS observations into AWS cloud fact envelopes.
//
// The package owns scanner-level ECS fact selection for clusters, services,
// task definitions, tasks, and relationships. AWS SDK pagination, credentials,
// persistence, graph projection, and reducer-owned correlation live outside
// this package.
package ecs
