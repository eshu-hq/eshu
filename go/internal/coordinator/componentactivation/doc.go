// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package componentactivation provides dependency-neutral parsing and
// validation for the generic component-extension activation configuration
// (`eshu.component.instance.v1`) collector instances carry in their
// Configuration field.
//
// The type is a shared contract, not a scheduler request: root's component
// registry readback constructs it, the componentextensionplanner child
// parses it back at planning time, and unrelated root files (the PagerDuty
// scheduler's exclusion check, the governance-audit event builder) read it
// to detect or identify a component-extension instance. This package
// imports only internal/component, never internal/coordinator or any
// coordinator child package, so every one of those consumers — coordinator
// root files and componentextensionplanner alike — can import it without
// creating an import cycle.
package componentactivation
