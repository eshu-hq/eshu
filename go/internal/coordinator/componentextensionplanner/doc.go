// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package componentextensionplanner builds deterministic workflow rows for
// one claim-capable generic component extension instance.
//
// The planner parses the collector instance's `eshu.component.instance.v1`
// activation configuration through componentactivation.ParseConfig, derives
// claim identity from an optional host claim or the component identity
// otherwise, and emits requested-scope metadata that omits raw host
// configuration paths and credentials. It performs no network, credential,
// or database work, and it never imports the root coordinator package: the
// shared activation-configuration contract lives in the dependency-neutral
// componentactivation package, not here, so root's unrelated callers of
// that contract (the PagerDuty scheduler, the governance-audit event
// builder) never need to depend on this package. Callers retain
// responsibility for clocks, scheduling, hosted extension egress
// authorization, durable admission, retries, queue and lease behavior, and
// telemetry.
package componentextensionplanner
