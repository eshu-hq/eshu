// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package activation provides dependency-neutral parsing and
// validation for the generic component-extension activation configuration
// (`eshu.component.instance.v1`) collector instances carry in their
// Configuration field.
//
// The type is a shared contract, not a scheduler request: root's component
// registry readback constructs it, the component-extension service checks it,
// the extension planner parses it at planning time, and unrelated root files
// (the PagerDuty scheduler's exclusion check and the governance-audit event
// builder) read it to detect or identify a component-extension instance. This package
// imports only internal/component, never internal/coordinator or any
// coordinator child package, so every one of those consumers — coordinator
// root files and extension alike — can import it without
// creating an import cycle.
//
// Config.ComponentConfig is the one opaque field: the activation's config
// block as a JSON object, filled by root's readback only for a collector kind
// whose coordinator plans host-derived scopes (#9311). ParseConfig accepts it
// absent, null, or as any JSON object and rejects every other JSON value; it
// never reads a key inside it.
package activation
